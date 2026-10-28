//go:build zvec

package zvec

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	zvec "github.com/zvec-ai/zvec-go"
)

var (
	initOnce sync.Once
	initErr  error
)

// compactionWriteThreshold is the number of native write/delete operations after
// which a periodic compaction runs. zvec finalizes its memory component and
// allocates a fresh ~5 MB .proxima block on every Flush, so an unmaintained index
// grows one block per upsert batch. The value is a starting compromise, not a
// guarantee of bounded size or time on an arbitrary index: too small multiplies
// full rebuilds over a growing index, too large leaves the peak footprint high.
const compactionWriteThreshold = 128

// CollectionStore is a zvec-go backed vector store.
type CollectionStore struct {
	cfg        Config
	mu         sync.Mutex
	col        *zvec.Collection
	open       bool
	readOnly   bool
	collection string
	path       string
	// closeHook, when set, replaces col.Close (tests).
	closeHook func() error
	// compactHook, when set, replaces col.Optimize (tests).
	compactHook func() error
	// dirty reports that a native write/delete may have changed the collection
	// since the last successful compaction. Only a dirty collection is optimized,
	// so an unchanged run never rebuilds the index.
	dirty bool
	// writeOps counts native write/delete operations since the last successful
	// compaction, or since the last failed attempt restarted the interval.
	writeOps int
}

var _ Compactor = (*CollectionStore)(nil)

// New creates a zvec-backed store.
func New(cfg Config) Store {
	return &CollectionStore{
		cfg:        cfg,
		collection: collectionNameWithSuffix(cfg.WorkspaceRoot, cfg.ProfileName, cfg.Dimensions, cfg.CollectionSuffix),
		path:       CollectionPath(cfg),
	}
}

func ensureInit() error {
	initOnce.Do(func() {
		initErr = zvec.Initialize(nil)
	})
	return initErr
}

// Open opens the collection read-only. Idempotent in the same process.
func (s *CollectionStore) Open() error {
	return s.openCollection(true)
}

// OpenWritable opens the collection for writing, creating it when absent.
func (s *CollectionStore) OpenWritable() error {
	return s.ensureWritable()
}

// IsOpen reports whether this process holds an open collection handle.
func (s *CollectionStore) IsOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.open && s.col != nil
}

// Close closes the collection handle.
func (s *CollectionStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.col == nil && s.closeHook == nil {
		s.open = false
		return nil
	}
	if closeErr := s.closeCollectionLocked(); closeErr != nil {
		return closeErr
	}
	s.col = nil
	s.open = false
	return nil
}

func (s *CollectionStore) closeCollectionLocked() error {
	if s.closeHook != nil {
		return s.closeHook()
	}
	if s.col == nil {
		return nil
	}
	return s.col.Close()
}

// DocCount returns the number of documents in the collection.
func (s *CollectionStore) DocCount() (int, error) {
	if err := s.Open(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.col == nil {
		return 0, ErrCollectionMissing
	}
	stats, err := s.col.GetStats()
	if err != nil {
		return 0, err
	}
	return int(stats.DocCount), nil
}

// DocIDsPresent reports whether every id exists in the collection.
func (s *CollectionStore) DocIDsPresent(ids []string) (bool, error) {
	if len(ids) == 0 {
		return true, nil
	}
	if err := s.Open(); err != nil {
		return false, err
	}
	const batchSize = 128
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.col == nil {
		return false, ErrCollectionMissing
	}
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		docs, err := s.col.Fetch(batch, &zvec.FetchOptions{})
		if err != nil {
			return false, err
		}
		found := 0
		for _, doc := range docs {
			if doc != nil {
				found++
				doc.Destroy()
			}
		}
		if found < len(batch) {
			return false, nil
		}
	}
	return true, nil
}

// UpsertChunks inserts or updates chunks with vectors.
func (s *CollectionStore) UpsertChunks(chunks []Chunk, vectors [][]float32) error {
	if len(chunks) != len(vectors) {
		return fmt.Errorf("chunks/vectors length mismatch: %d vs %d", len(chunks), len(vectors))
	}
	if len(chunks) == 0 {
		return nil
	}
	if err := s.ensureWritable(); err != nil {
		return err
	}

	docs := make([]*zvec.Doc, 0, len(chunks))
	defer func() {
		for _, d := range docs {
			if d != nil {
				d.Destroy()
			}
		}
	}()

	for i, chunk := range chunks {
		if len(vectors[i]) != s.cfg.Dimensions {
			return fmt.Errorf("vector %d: dimension mismatch got %d want %d", i, len(vectors[i]), s.cfg.Dimensions)
		}
		doc, err := chunkToDoc(chunk, vectors[i])
		if err != nil {
			return err
		}
		docs = append(docs, doc)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.col == nil {
		return ErrCollectionMissing
	}
	wr, err := s.col.Upsert(docs)
	if err != nil {
		// A native batch error is ambiguous: part of the batch may be applied, so
		// keep the collection marked for maintenance. There is no durability flush
		// on this branch, hence no periodic compaction.
		s.writeOutcomeLocked(true, false)
		return err
	}
	successCount := clampUint64ToInt(wr.SuccessCount)
	errorCount := clampUint64ToInt(wr.ErrorCount)
	succeeded := docIDsFromChunks(chunks, successCount)
	if errorCount > 0 {
		s.writeOutcomeLocked(true, false)
		return &PartialWriteError{
			Op:        "upsert",
			Succeeded: succeeded,
			Failed:    errorCount,
			Cause:     fmt.Errorf("upsert: %d errors (success %d)", errorCount, successCount),
		}
	}
	if flushErr := s.col.Flush(); flushErr != nil {
		s.writeOutcomeLocked(true, false)
		return &FlushWriteError{Op: "upsert", Succeeded: succeeded, Cause: flushErr}
	}
	s.writeOutcomeLocked(true, true)
	return nil
}

// DeleteByIDs removes documents by primary key.
//
// It is idempotent: a key that is already absent has reached the delete goal, so a
// repeated delete (a cleanup journal replay, a retry) succeeds instead of reporting a
// partial failure. Native SuccessCount is a count, not an identity, so a batch that
// reported errors is resolved with a real Fetch rather than by assuming a prefix.
func (s *CollectionStore) DeleteByIDs(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.ensureWritable(); err != nil {
		return err
	}
	// Deduplicate into our own slice: the caller reuses ids after the call.
	requested := dedupeIDs(ids)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.col == nil {
		return ErrCollectionMissing
	}
	wr, err := s.col.Delete(requested)
	if err != nil {
		// Same ambiguity as upsert: do not assume the batch changed nothing.
		s.writeOutcomeLocked(true, false)
		return err
	}
	successCount := clampUint64ToInt(wr.SuccessCount)
	errorCount := clampUint64ToInt(wr.ErrorCount)

	if errorCount > 0 {
		slog.Debug("native delete reported errors", "collection", s.collection,
			"errors", errorCount, "success", successCount)
		// Errors here normally mean "already gone", which is success. Resolve which
		// requested ids actually survived instead of trusting SuccessCount ordering.
		remaining, fetchErr := s.remainingIDsLocked(requested)
		delErr := deleteError("delete", requested, remaining, fetchErr)
		if delErr == nil {
			// Every requested key is gone; durability still needs the flush below.
			if flushErr := s.col.Flush(); flushErr != nil {
				s.writeOutcomeLocked(true, false)
				return &FlushWriteError{Op: "delete", Succeeded: requested, Cause: flushErr}
			}
			s.writeOutcomeLocked(true, true)
			return nil
		}
		s.writeOutcomeLocked(true, false)
		return delErr
	}

	if flushErr := s.col.Flush(); flushErr != nil {
		s.writeOutcomeLocked(true, false)
		return &FlushWriteError{Op: "delete", Succeeded: requested, Cause: flushErr}
	}
	s.writeOutcomeLocked(true, true)
	return nil
}

// remainingIDsLocked reports which of ids still exist in the collection.
//
// It fetches without vectors and destroys every returned Doc. Callers must hold s.mu;
// the method exists so a batch that reported errors can be resolved under the same
// lock that serializes writes and Close.
func (s *CollectionStore) remainingIDsLocked(ids []string) ([]string, error) {
	const batchSize = 128
	var remaining []string
	// Only the primary key is needed. A nil OutputFields means "all fields", which
	// would materialize every snippet just to read the PK; the PK itself comes from
	// GetPK and is independent of OutputFields.
	opts := &zvec.FetchOptions{IncludeVector: false, OutputFields: []string{fieldName}}
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		docs, err := s.col.Fetch(ids[start:end], opts)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			if doc == nil {
				continue
			}
			remaining = append(remaining, doc.GetPK())
			doc.Destroy()
		}
	}
	return remaining, nil
}

// Search runs a vector similarity query with optional path glob filter.
func (s *CollectionStore) Search(vector []float32, topK int, pathGlob string) ([]SearchHit, error) {
	if topK <= 0 {
		topK = 10
	}
	if len(vector) != s.cfg.Dimensions {
		return nil, fmt.Errorf("query vector dimension mismatch: got %d want %d", len(vector), s.cfg.Dimensions)
	}
	if err := s.Open(); err != nil {
		return nil, err
	}

	queryTopK := topK
	if strings.TrimSpace(pathGlob) != "" {
		return s.searchWithGlobExpansion(vector, topK, pathGlob)
	}

	// Hold the lock across the whole query: the underlying CGO handle can be
	// closed by Close/WipeCollection or mutated by writes, so the read path must
	// be serialized with them to avoid a use-after-close / data race.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.col == nil {
		return nil, ErrCollectionMissing
	}

	q := zvec.NewSearchQuery()
	defer q.Destroy()
	if err := q.SetFieldName(fieldEmbedding); err != nil {
		return nil, err
	}
	if err := q.SetTopK(queryTopK); err != nil {
		return nil, err
	}
	if err := q.SetOutputFields(searchOutputFields); err != nil {
		return nil, err
	}
	if err := q.SetQueryVector(vector); err != nil {
		return nil, err
	}

	results, err := s.col.Query(q)
	if err != nil {
		return nil, err
	}
	defer zvec.FreeDocs(results)

	hits := make([]SearchHit, 0, len(results))
	for _, doc := range results {
		if doc == nil {
			continue
		}
		hit := docToSearchHit(doc)
		if !matchPathGlob(hit.Path, pathGlob) {
			continue
		}
		hits = append(hits, hit)
		if len(hits) >= topK {
			break
		}
	}
	return hits, nil
}

// Compact reclaims the on-disk blocks accumulated by per-write flushes.
//
// It is best-effort maintenance, not a durability step: a clean or missing
// collection is a no-op and no collection is created, so an unchanged indexing run
// never rebuilds the index. The mutex is held across the native call, which briefly
// delays search and writes on this collection.
func (s *CollectionStore) Compact() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compactLocked()
}

// writeOutcomeLocked records the result of one native write/delete operation.
//
// changed reports that the native call may have modified the collection (including
// partial and ambiguous outcomes); durable reports that a successful Flush already
// made it crash-safe. Only a durable outcome may trigger the periodic compaction, so
// the failed-flush branch never starts a rebuild.
//
// Callers must hold s.mu.
func (s *CollectionStore) writeOutcomeLocked(changed, durable bool) {
	if !changed {
		return
	}
	s.dirty = true
	s.writeOps++
	if !durable || s.writeOps < compactionWriteThreshold {
		return
	}
	if err := s.compactLocked(); err != nil {
		// Maintenance must never turn a successful write into a reported failure:
		// the index is still correct, only its footprint stays large.
		slog.Warn("zvec compaction failed; collection stays dirty",
			"collection", s.collection, "err", err)
	}
}

// compactLocked runs one maintenance pass. It requires s.mu to be held and must not
// call any public store method (those re-acquire the same mutex).
func (s *CollectionStore) compactLocked() error {
	if !s.dirty {
		return nil
	}
	if s.col == nil && s.compactHook == nil {
		return nil
	}
	if s.readOnly {
		// A read-only handle cannot rewrite the index. Attempting it would fail on every
		// run and never clear the dirty flag — a warning per run, forever. Keep the flag
		// and let the next writable session do the work.
		return nil
	}
	if err := s.optimizeLocked(); err != nil {
		// Keep dirty: the collection still needs maintenance. Restart the interval
		// so the next attempt waits a full threshold instead of firing on every
		// following batch.
		s.writeOps = 0
		return err
	}
	s.dirty = false
	s.writeOps = 0
	return nil
}

func (s *CollectionStore) optimizeLocked() error {
	if s.compactHook != nil {
		return s.compactHook()
	}
	return s.col.Optimize()
}

// WipeCollection removes the on-disk collection directory.
func (s *CollectionStore) WipeCollection() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.col != nil || s.closeHook != nil {
		if err := s.closeCollectionLocked(); err != nil {
			return fmt.Errorf("close before wipe: %w", err)
		}
		s.col = nil
	}
	s.open = false
	s.readOnly = false
	if err := os.RemoveAll(s.path); err != nil {
		return fmt.Errorf("remove collection: %w", err)
	}
	// The collection the counters described is gone: the next build starts clean.
	s.dirty = false
	s.writeOps = 0
	return nil
}

func docIDsFromChunks(chunks []Chunk, successCount int) []string {
	if successCount <= 0 {
		return nil
	}
	if successCount >= len(chunks) {
		out := make([]string, len(chunks))
		for i, ch := range chunks {
			out[i] = ch.DocID
		}
		return out
	}
	out := make([]string, 0, successCount)
	for i := 0; i < successCount && i < len(chunks); i++ {
		out = append(out, chunks[i].DocID)
	}
	return out
}

func clampUint64ToInt(n uint64) int {
	const maxInt = int(^uint(0) >> 1)
	if n > uint64(maxInt) {
		return maxInt
	}
	return int(n)
}

// searchWithGlobExpansion filters hits in Go after querying with an expanded topK.
// Native SearchQuery.SetFilter is not used: path_glob supports ** / nested globs whose
// semantics are implemented by matchPathGlob; expanding topK until enough matches
// (or the collection is exhausted) preserves recall without a native glob filter API.
func (s *CollectionStore) searchWithGlobExpansion(vector []float32, topK int, pathGlob string) ([]SearchHit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.col == nil {
		return nil, ErrCollectionMissing
	}
	docCount, err := s.docCountLocked()
	if err != nil {
		return nil, err
	}
	if docCount == 0 {
		return nil, nil
	}
	// Start at min(topK, docCount): with topK > docCount the old schedule never
	// ran a single query and returned zero hits.
	queryTopK := globTopKStart(topK, docCount)
	var hits []SearchHit
	for len(hits) < topK && queryTopK > 0 && queryTopK <= docCount {
		results, qErr := s.queryLocked(vector, queryTopK)
		if qErr != nil {
			return nil, qErr
		}
		hits = hits[:0]
		for _, doc := range results {
			if doc == nil {
				continue
			}
			hit := docToSearchHit(doc)
			if !matchPathGlob(hit.Path, pathGlob) {
				continue
			}
			hits = append(hits, hit)
			if len(hits) >= topK {
				break
			}
		}
		zvec.FreeDocs(results)
		if len(hits) >= topK || queryTopK >= docCount {
			break
		}
		// Doubling is capped at docCount so the last iteration is a full pass;
		// an uncapped step (e.g. 64 -> 128 with docCount 121) would skip it and
		// lose matches ranked below the previous window.
		queryTopK = nextGlobTopK(queryTopK, docCount)
	}
	return hits, nil
}

func (s *CollectionStore) docCountLocked() (int, error) {
	stats, err := s.col.GetStats()
	if err != nil {
		return 0, err
	}
	return int(stats.DocCount), nil
}

func (s *CollectionStore) queryLocked(vector []float32, queryTopK int) ([]*zvec.Doc, error) {
	q := zvec.NewSearchQuery()
	defer q.Destroy()
	if err := q.SetFieldName(fieldEmbedding); err != nil {
		return nil, err
	}
	if err := q.SetTopK(queryTopK); err != nil {
		return nil, err
	}
	if err := q.SetOutputFields(searchOutputFields); err != nil {
		return nil, err
	}
	if err := q.SetQueryVector(vector); err != nil {
		return nil, err
	}
	return s.col.Query(q)
}

func (s *CollectionStore) ensureWritable() error {
	if err := s.openCollection(false); err != nil {
		return err
	}
	s.mu.Lock()
	readOnly := s.readOnly
	s.mu.Unlock()
	if readOnly {
		return fmt.Errorf("collection opened read-only")
	}
	return nil
}

func (s *CollectionStore) openCollection(readOnly bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.open && s.col != nil {
		if readOnly || !s.readOnly {
			return nil
		}
		// zvec holds an exclusive LOCK per mode: RW open fails while RO is still open.
		oldCol := s.col
		s.col = nil
		s.open = false
		if err := oldCol.Close(); err != nil {
			s.col = oldCol
			s.open = true
			s.readOnly = true
			return err
		}
		col, err := s.tryOpen(false)
		if err != nil {
			if roCol, roErr := s.tryOpen(true); roErr == nil {
				s.col = roCol
				s.open = true
				s.readOnly = true
			}
			return err
		}
		s.col = col
		s.open = true
		s.readOnly = false
		return nil
	}

	if err := ensureInit(); err != nil {
		return fmt.Errorf("zvec initialize: %w", err)
	}

	if _, err := os.Stat(s.path); err != nil {
		if os.IsNotExist(err) {
			if readOnly {
				return ErrCollectionMissing
			}
			return s.createCollection()
		}
		return fmt.Errorf("stat collection path: %w", err)
	}

	col, err := s.tryOpen(readOnly)
	if err != nil {
		return err
	}
	s.col = col
	s.open = true
	s.readOnly = readOnly
	return nil
}

func (s *CollectionStore) createCollection() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir zvec parent: %w", err)
	}
	schema, err := buildSchema(s.collection, s.cfg.Dimensions)
	if err != nil {
		return err
	}
	defer schema.Destroy()

	opts := s.newCollectionOptions(false)
	defer opts.Destroy()
	col, err := zvec.CreateAndOpen(s.path, schema, opts)
	if err != nil {
		return err
	}
	s.col = col
	s.open = true
	s.readOnly = false
	return nil
}

func (s *CollectionStore) tryOpen(readOnly bool) (*zvec.Collection, error) {
	openWith := func() (*zvec.Collection, error) {
		opts := s.newCollectionOptions(readOnly)
		if opts == nil {
			return nil, fmt.Errorf("create collection options")
		}
		defer opts.Destroy()
		return zvec.Open(s.path, opts)
	}

	col, err := openWith()
	if err == nil {
		return col, nil
	}
	if reclaimStaleLock(s.path) {
		return openWith()
	}
	return nil, err
}

func (s *CollectionStore) newCollectionOptions(readOnly bool) *zvec.CollectionOptions {
	opts := zvec.NewCollectionOptions()
	if opts == nil {
		return nil
	}
	if err := opts.SetReadOnly(readOnly); err != nil {
		opts.Destroy()
		return nil
	}
	if err := opts.SetEnableMmap(collectionMmapEnabled(s.path)); err != nil {
		opts.Destroy()
		return nil
	}
	return opts
}

func reclaimStaleLock(collectionPath string) bool {
	reclaimed := reclaimCollectionLockDir(collectionPath)
	if reclaimed {
		slog.Info("reclaimed orphaned zvec collection lock", "path", collectionPath)
	}
	return reclaimed
}

// IsCollectionMissing reports whether err is ErrCollectionMissing.
func IsCollectionMissing(err error) bool {
	return errors.Is(err, ErrCollectionMissing)
}
