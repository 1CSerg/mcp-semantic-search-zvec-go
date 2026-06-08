//go:build zvec

package zvec

import (
	"errors"
	"fmt"
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
}

// New creates a zvec-backed store.
func New(cfg Config) Store {
	return &CollectionStore{
		cfg:        cfg,
		collection: CollectionName(cfg.WorkspaceRoot, cfg.ProfileName, cfg.Dimensions),
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

// Close closes the collection handle.
func (s *CollectionStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.col != nil {
		s.col.Close()
		s.col = nil
	}
	s.open = false
	return nil
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
	wr, err := s.col.Upsert(docs)
	if err != nil {
		return err
	}
	if wr.ErrorCount > 0 {
		return fmt.Errorf("upsert: %d errors (success %d)", wr.ErrorCount, wr.SuccessCount)
	}
	return s.col.Flush()
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
	wr, err := s.col.Delete(ids)
	if err != nil {
		return err
	}
	if wr.ErrorCount > 0 {
		return fmt.Errorf("delete: %d errors (success %d)", wr.ErrorCount, wr.SuccessCount)
	}
	return s.col.Flush()
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
		queryTopK = topK * 4
		if queryTopK < topK {
			queryTopK = topK
		}
	}

	s.mu.Lock()
	col := s.col
	s.mu.Unlock()
	if col == nil {
		return nil, ErrCollectionMissing
	}

	q := zvec.NewVectorQuery()
	defer q.Destroy()
	if err := q.SetFieldName(fieldEmbedding); err != nil {
		return nil, err
	}
	if err := q.SetTopK(queryTopK); err != nil {
		return nil, err
	}
	if err := q.SetOutputFields([]string{fieldPath, fieldStartLine, fieldEndLine, fieldChunkType, fieldName, fieldSnippet}); err != nil {
		return nil, err
	}
	if err := q.SetQueryVector(vector); err != nil {
		return nil, err
	}

	results, err := col.Query(q)
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
	if err := s.Close(); err != nil {
		return err
	}
	if err := os.RemoveAll(s.path); err != nil {
		return fmt.Errorf("remove collection: %w", err)
	}
	s.open = false
	s.readOnly = false
	return nil
}

func (s *CollectionStore) ensureWritable() error {
	if err := s.openCollection(false); err != nil {
		return err
	}
	if s.readOnly {
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
		s.col.Close()
		s.col = nil
		s.open = false
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

	col, err := zvec.CreateAndOpen(s.path, schema, nil)
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
		opts := zvec.NewCollectionOptions()
		if opts == nil {
			return nil, fmt.Errorf("create collection options")
		}
		defer opts.Destroy()
		if err := opts.SetReadOnly(readOnly); err != nil {
			return nil, err
		}
		if err := opts.SetEnableMmap(true); err != nil {
			return nil, err
		}
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

func reclaimStaleLock(collectionPath string) bool {
	removed := false
	entries, err := os.ReadDir(collectionPath)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if name == "LOCK" || name == "lock" || name == ".lock" {
			if err := os.Remove(filepath.Join(collectionPath, name)); err == nil {
				removed = true
			}
		}
	}
	return removed
}

// IsCollectionMissing reports whether err is ErrCollectionMissing.
func IsCollectionMissing(err error) bool {
	return errors.Is(err, ErrCollectionMissing)
}
