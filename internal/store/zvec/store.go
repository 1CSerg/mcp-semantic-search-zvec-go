package zvec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
)

// ErrNotLinked is returned until zvec-go CGO integration completes (Phase 1 spike).
var ErrNotLinked = fmt.Errorf("zvec store not linked: complete docs/ZVEC_SPIKE.md and enable CGO build")

// Chunk is a searchable document slice stored in zvec.
type Chunk struct {
	DocID        string
	RelativePath string
	StartLine    int64
	EndLine      int64
	ChunkType    string
	Name         string
	Snippet      string
}

// SearchHit is one query result.
type SearchHit struct {
	DocID     string
	Path      string
	StartLine int64
	EndLine   int64
	ChunkType string
	Name      string
	Snippet   string
	Score     float64
}

// Store abstracts zvec collection operations.
type Store interface {
	Open() error
	Close() error
	DocCount() (int, error)
	UpsertChunks(chunks []Chunk, vectors [][]float32) error
	DeleteByIDs(ids []string) error
	Search(vector []float32, topK int, pathGlob string) ([]SearchHit, error)
}

// CollectionName derives stable zvec collection directory name.
func CollectionName(workspaceRoot, profileName string, dimensions int) string {
	raw := fmt.Sprintf("%s:%s:%d", filepath.Clean(workspaceRoot), profileName, dimensions)
	sum := sha256.Sum256([]byte(raw))
	return "ws_" + hex.EncodeToString(sum[:])[:16]
}

// StubStore is a placeholder until zvec-go is integrated.
type StubStore struct{}

func NewStub() *StubStore { return &StubStore{} }

func (s *StubStore) Open() error                             { return ErrNotLinked }
func (s *StubStore) Close() error                            { return nil }
func (s *StubStore) DocCount() (int, error)                  { return 0, ErrNotLinked }
func (s *StubStore) UpsertChunks([]Chunk, [][]float32) error { return ErrNotLinked }
func (s *StubStore) DeleteByIDs([]string) error              { return ErrNotLinked }
func (s *StubStore) Search([]float32, int, string) ([]SearchHit, error) {
	return nil, ErrNotLinked
}
