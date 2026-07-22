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
}

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
		return err
	}
	successCount := clampUint64ToInt(wr.SuccessCount)
	errorCount := clampUint64ToInt(wr.ErrorCount)
	succeeded := docIDsFromChunks(chunks, successCount)
	if errorCount > 0 {
		return &PartialWriteError{
			Op:        "upsert",
			Succeeded: succeeded,
			Failed:    errorCount,
			Cause:     fmt.Errorf("upsert: %d errors (success %d)", errorCount, successCount),
		}
	}
	if flushErr := s.col.Flush(); flushErr != nil {
		return &FlushWriteError{Op: "upsert", Succeeded: succeeded, Cause: flushErr}
	}
	return nil
}

// DeleteByIDs removes documents by primary key.
func (s *CollectionStore) DeleteByIDs(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.ensureWritable(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.col == nil {
		return ErrCollectionMissing
	}
	wr, err := s.col.Delete(ids)
	if err != nil {
		return err
	}
	successCount := clampUint64ToInt(wr.SuccessCount)
	errorCount := clampUint64ToInt(wr.ErrorCount)
	succeeded := ids[:0]
	if successCount > 0 {
		if successCount >= len(ids) {
			succeeded = append(succeeded, ids...)
		} else {
			succeeded = append(succeeded, ids[:successCount]...)
		}
	}
	if errorCount > 0 {
		return &PartialWriteError{
			Op:        "delete",
			Succeeded: succeeded,
			Failed:    errorCount,
			Cause:     fmt.Errorf("delete: %d errors (success %d)", errorCount, successCount),
		}
	}
	if flushErr := s.col.Flush(); flushErr != nil {
		return &FlushWriteError{Op: "delete", Succeeded: succeeded, Cause: flushErr}
	}
	return nil
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
	queryTopK := topK
	var hits []SearchHit
	for len(hits) < topK && queryTopK <= docCount {
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
		next := queryTopK * 2
		if next <= queryTopK {
			next = docCount
		}
		queryTopK = next
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
