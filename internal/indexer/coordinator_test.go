package indexer

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

type mockEmbedder struct {
	dims int
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, m.dims)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

func (m *mockEmbedder) Dimensions() int { return m.dims }

type memZvec struct {
	mu     sync.Mutex
	chunks map[string]zvec.Chunk
}

func newMemZvec() *memZvec {
	return &memZvec{chunks: map[string]zvec.Chunk{}}
}

func (s *memZvec) Open() error  { return nil }
func (s *memZvec) Close() error { return nil }
func (s *memZvec) DocCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.chunks), nil
}
func (s *memZvec) UpsertChunks(chunks []zvec.Chunk, _ [][]float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range chunks {
		s.chunks[ch.DocID] = ch
	}
	return nil
}
func (s *memZvec) DeleteByIDs(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		delete(s.chunks, id)
	}
	return nil
}
func (s *memZvec) Search([]float32, int, string) ([]zvec.SearchHit, error) { return nil, nil }
func (s *memZvec) WipeCollection() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = map[string]zvec.Chunk{}
	return nil
}

func TestCoordinatorIndexesFile(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("package pkg\n\nfunc Auth() {}\n")
	if err := os.WriteFile(filepath.Join(root, "pkg", "auth.go"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	settings := &config.Settings{
		WorkspaceRoot: root,
		WorkspaceID:   "test-ws",
		IndexDir:      indexDir,
		App: config.AppConfig{
			ActiveProfile: "test",
			Indexing: config.IndexingConfig{
				Extensions:       []string{".go"},
				SkipDirs:         []string{".git", "node_modules"},
				LockStaleSeconds: 300,
				HeartbeatSeconds: 15,
			},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 8}
	store := newMemZvec()
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 8}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 8}, store, cfg)

	p, err := c.Start(true)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Running {
		t.Fatalf("progress=%+v", p)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		cur := c.CurrentProgress()
		if !cur.Running && cur.State == StateIdle {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cur := c.CurrentProgress()
	if cur.Running || cur.State != StateIdle {
		t.Fatalf("final progress=%+v", cur)
	}
	if cur.Error != "" {
		t.Fatalf("index error: %s", cur.Error)
	}

	man, err := manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer man.Close()
	files, chunks, err := man.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 || chunks == 0 {
		t.Fatalf("files=%d chunks=%d", files, chunks)
	}
	if n, _ := store.DocCount(); n == 0 {
		t.Fatal("expected zvec chunks")
	}
}

func TestCoordinatorAlreadyRunning(t *testing.T) {
	root := t.TempDir()
	settings := &config.Settings{
		WorkspaceRoot: root,
		WorkspaceID:   "test-ws",
		IndexDir:      filepath.Join(root, "index"),
		App: config.AppConfig{
			ActiveProfile: "test",
			Indexing: config.IndexingConfig{
				Extensions:       []string{".go"},
				LockStaleSeconds: 300,
			},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4}
	store := newMemZvec()
	cfg := zvec.Config{IndexDir: settings.IndexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, store, cfg)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	if !c.IsRunning() {
		t.Fatal("expected running")
	}
	if _, err := c.Start(true); err == nil {
		t.Fatal("expected already running error")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && c.IsRunning() {
		time.Sleep(20 * time.Millisecond)
	}
}

func TestIsZvecUnavailable(t *testing.T) {
	if !isZvecUnavailable(zvec.ErrNotLinked) {
		t.Fatal("expected unavailable")
	}
	if isZvecUnavailable(nil) {
		t.Fatal("expected available")
	}
}

func TestCoordinatorStartFailsWhenLockHeld(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	extLock := lock.New(indexDir, 300)
	if err := extLock.TryAcquire(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = extLock.Release() }()

	settings := &config.Settings{
		WorkspaceRoot: root,
		WorkspaceID:   "test-ws",
		IndexDir:      indexDir,
		App: config.AppConfig{
			ActiveProfile: "test",
			Indexing: config.IndexingConfig{
				Extensions:       []string{".go"},
				LockStaleSeconds: 300,
			},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4}
	store := newMemZvec()
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, store, cfg)

	_, err := c.Start(true)
	if err == nil {
		t.Fatal("expected lock contention error")
	}
	if c.IsRunning() {
		t.Fatal("coordinator should not be running")
	}
	p := c.CurrentProgress()
	if p.Running {
		t.Fatalf("progress should not be running: %+v", p)
	}
}
