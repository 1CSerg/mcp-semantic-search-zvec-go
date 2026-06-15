package indexer

import (
	"context"
	"fmt"
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

func TestCoordinatorStartRecoversStaleProgress(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	store := NewProgressStore(indexDir)
	if err := store.Save(StartRunning(false)); err != nil {
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
				LockStaleSeconds: 300,
				StallSeconds:     120,
			},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, newMemZvec(), zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: root,
		ProfileName:   "test",
		Dimensions:    4,
	})
	pgr, err := c.Start(false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !pgr.Running {
		t.Fatal("expected running progress")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && c.IsRunning() {
		time.Sleep(20 * time.Millisecond)
	}
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

func TestCoordinatorSkipsUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "auth.go"), []byte("package pkg\n"), 0o644); err != nil {
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
				LockStaleSeconds: 300,
			},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4}
	store := newMemZvec()
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, store, cfg)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && c.IsRunning() {
		time.Sleep(20 * time.Millisecond)
	}
	calls := 0
	embed := &countingEmbedder{mockEmbedder: mockEmbedder{dims: 4}, calls: &calls}
	c.Embed = embed
	if _, err := c.Start(false); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) && c.IsRunning() {
		time.Sleep(20 * time.Millisecond)
	}
	if calls != 0 {
		t.Fatalf("expected no embed calls for unchanged file, got %d", calls)
	}
}

type countingEmbedder struct {
	mockEmbedder
	calls *int
}

func (c *countingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	*c.calls++
	return c.mockEmbedder.Embed(ctx, texts)
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

func TestCoordinatorCurrentProgressLoadError(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "progress.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := &config.Settings{
		WorkspaceRoot: root,
		WorkspaceID:   "test-ws",
		IndexDir:      indexDir,
		App: config.AppConfig{
			ActiveProfile: "test",
			Indexing:      config.IndexingConfig{Extensions: []string{".go"}, LockStaleSeconds: 300},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, newMemZvec(), zvec.Config{
		IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4,
	})
	p := c.CurrentProgress()
	if p.Message == "" {
		t.Fatalf("progress=%+v", p)
	}
}

func TestCoordinatorStartProgressSaveFails(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexDir := filepath.Join(blocked, "index")
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
	if _, err := c.Start(true); err == nil {
		t.Fatal("expected progress save error")
	}
	if c.IsRunning() {
		t.Fatal("coordinator should not be running")
	}
}

func TestCoordinatorIncrementalRemovesDeletedFile(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "pkg", "auth.go")
	if err := os.WriteFile(path, []byte("package pkg\n"), 0o644); err != nil {
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
				LockStaleSeconds: 300,
			},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4}
	store := newMemZvec()
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, store, cfg)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && c.IsRunning() {
		time.Sleep(20 * time.Millisecond)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Start(false); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) && c.IsRunning() {
		time.Sleep(20 * time.Millisecond)
	}
	man, err := manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer man.Close()
	if _, err := man.Get("pkg/auth.go"); err == nil {
		t.Fatal("expected deleted file removed from manifest")
	}
}

func TestCoordinatorEmbedFailure(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.WriteFile(filepath.Join(root, "bad.go"), []byte("package main\n"), 0o644); err != nil {
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
				LockStaleSeconds: 300,
			},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4}
	store := newMemZvec()
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &failingEmbedder{}, store, cfg)
	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && c.IsRunning() {
		time.Sleep(20 * time.Millisecond)
	}
	p := c.CurrentProgress()
	if p.State != StateError {
		t.Fatalf("progress=%+v", p)
	}
}

type failingEmbedder struct{}

func (failingEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, os.ErrInvalid
}

func (failingEmbedder) Dimensions() int { return 4 }

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

func TestCoordinatorForceReindexOwnerMismatch(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	oldRoot := filepath.Join(root, "old-path")
	newRoot := root
	oldCollection := zvec.CollectionName(oldRoot, "test", 4)
	if err := os.MkdirAll(filepath.Join(indexDir, "zvec", oldCollection), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{
		WorkspaceID:         "ws-a",
		WorkspaceRoot:       oldRoot,
		EmbeddingProfile:    "test",
		EmbeddingDimensions: 4,
		CollectionName:      oldCollection,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	settings := &config.Settings{
		WorkspaceRoot: newRoot,
		WorkspaceID:   "ws-b",
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
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: newRoot, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, store, cfg)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && c.IsRunning() {
		time.Sleep(20 * time.Millisecond)
	}
	p := c.CurrentProgress()
	if p.State != StateIdle {
		t.Fatalf("progress=%+v", p)
	}
	meta, err := zvec.ReadIndexMeta(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.WorkspaceID != "ws-b" {
		t.Fatalf("meta=%+v", meta)
	}
	if _, err := os.Stat(filepath.Join(indexDir, "zvec", oldCollection)); !os.IsNotExist(err) {
		t.Fatalf("old collection still present: err=%v", err)
	}
}

type selectiveFailZvec struct {
	*memZvec
	failPaths map[string]struct{}
}

func newSelectiveFailZvec(fail ...string) *selectiveFailZvec {
	m := make(map[string]struct{}, len(fail))
	for _, p := range fail {
		m[p] = struct{}{}
	}
	return &selectiveFailZvec{memZvec: newMemZvec(), failPaths: m}
}

func (s *selectiveFailZvec) UpsertChunks(chunks []zvec.Chunk, _ [][]float32) error {
	for _, ch := range chunks {
		if _, ok := s.failPaths[ch.RelativePath]; ok {
			return fmt.Errorf(`zvec error [INTERNAL_ERROR]: Invalid: File is too small: 6`)
		}
	}
	return s.memZvec.UpsertChunks(chunks, nil)
}

func TestCoordinatorSkipsPerFileZvecError(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".cursor", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "ok.go"), []byte("package pkg\n\nfunc OK() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cursor", "rules", "rule.mdc"), []byte("---\ntitle: rule\n---\n\n# Rule\n\ncontent\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	settings := &config.Settings{
		WorkspaceRoot: root,
		WorkspaceID:   "test-ws",
		IndexDir:      indexDir,
		App: config.AppConfig{
			ActiveProfile: "test",
			Indexing: config.IndexingConfig{
				Extensions:       []string{".go", ".mdc"},
				SkipDirs:         []string{".git", "node_modules"},
				LockStaleSeconds: 300,
			},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4}
	store := newSelectiveFailZvec(".cursor/rules/rule.mdc")
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, store, cfg)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && c.IsRunning() {
		time.Sleep(20 * time.Millisecond)
	}
	p := c.CurrentProgress()
	if p.State != StateIdle {
		t.Fatalf("progress=%+v", p)
	}
	if p.FilesFailed != 1 {
		t.Fatalf("files_failed=%d", p.FilesFailed)
	}
	m := p.ToIndexingMap()
	if _, ok := m["file_errors"]; ok {
		t.Fatalf("file_errors should not be in index_status: map=%v", m)
	}

	man, err := manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer man.Close()
	if _, err := man.Get("pkg/ok.go"); err != nil {
		t.Fatalf("ok.go not indexed: %v", err)
	}
	if _, err := man.Get(".cursor/rules/rule.mdc"); err == nil {
		t.Fatal("failed file should not be in manifest")
	}
}
