package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
func (s *memZvec) IsOpen() bool { return true }
func (s *memZvec) Close() error { return nil }
func (s *memZvec) DocCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.chunks), nil
}
func (s *memZvec) DocIDsPresent(ids []string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if _, ok := s.chunks[id]; !ok {
			return false, nil
		}
	}
	return true, nil
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

func waitCoordinatorIdle(t *testing.T, c *Coordinator) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		p := c.CurrentProgress()
		if !c.IsRunning() && (p.State == StateIdle || p.State == StateError) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("coordinator did not become idle")
}

func releaseCoordinatorTestResources(c *Coordinator) {
	_ = c.lock.Release()
	_ = c.Zvec.Close()
}

func registerCoordinatorTestCleanup(t *testing.T, c *Coordinator) {
	t.Helper()
	t.Setenv("MANIFEST_WAL", "off")
	t.Cleanup(func() {
		waitCoordinatorIdle(t, c)
		releaseCoordinatorTestResources(c)
	})
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
	registerCoordinatorTestCleanup(t, c)
	pgr, err := c.Start(false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !pgr.Running {
		t.Fatal("expected running progress")
	}
	waitCoordinatorIdle(t, c)
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
	registerCoordinatorTestCleanup(t, c)

	p, err := c.Start(true)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Running {
		t.Fatalf("progress=%+v", p)
	}

	waitCoordinatorIdle(t, c)
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

func TestCoordinatorReindexesWhenContentHashMissingOrChanged(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	rel := "pkg/auth.go"
	path := filepath.Join(root, rel)
	initial := []byte("package pkg\n\nfunc Auth() {}\n")
	if err := os.WriteFile(path, initial, 0o644); err != nil {
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
	registerCoordinatorTestCleanup(t, c)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)

	man, err := manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := man.Get(rel)
	if err != nil {
		t.Fatalf("manifest get: %v", err)
	}
	if entry.ContentHash == "" {
		t.Fatal("expected content hash after initial index")
	}
	originalHash := entry.ContentHash

	t.Run("legacy empty ContentHash", func(t *testing.T) {
		if err := man.Upsert(manifest.FileEntry{
			RelativePath: rel,
			MtimeNs:      entry.MtimeNs,
			Size:         entry.Size,
			ChunkCount:   entry.ChunkCount,
			DocIDs:       entry.DocIDs,
			ContentHash:  "",
		}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		embed := &countingEmbedder{mockEmbedder: mockEmbedder{dims: 4}, calls: &calls}
		c.Embed = embed
		if _, err := c.Start(false); err != nil {
			t.Fatal(err)
		}
		waitCoordinatorIdle(t, c)
		if calls == 0 {
			t.Fatal("expected reindex when legacy ContentHash is empty")
		}
		updated, err := man.Get(rel)
		if err != nil {
			t.Fatal(err)
		}
		if updated.ContentHash == "" {
			t.Fatal("expected ContentHash backfilled after reindex")
		}
		if updated.ContentHash != originalHash {
			t.Fatalf("content hash=%q want %q", updated.ContentHash, originalHash)
		}
	})

	t.Run("same mtime and size different bytes", func(t *testing.T) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		mtime := info.ModTime()
		size := info.Size()
		replacement := []byte("package pkg\n\nfunc Axth() {}\n")
		if int64(len(replacement)) != size {
			t.Fatalf("replacement size=%d want %d", len(replacement), size)
		}
		if err := os.WriteFile(path, replacement, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		newHash, err := fileContentHash(path)
		if err != nil {
			t.Fatal(err)
		}
		if newHash == originalHash {
			t.Fatal("expected different content hash after byte swap")
		}

		calls := 0
		embed := &countingEmbedder{mockEmbedder: mockEmbedder{dims: 4}, calls: &calls}
		c.Embed = embed
		if _, err := c.Start(false); err != nil {
			t.Fatal(err)
		}
		waitCoordinatorIdle(t, c)
		if calls == 0 {
			t.Fatal("expected reindex when content changed but mtime and size match")
		}
		updated, err := man.Get(rel)
		if err != nil {
			t.Fatal(err)
		}
		if updated.ContentHash != newHash {
			t.Fatalf("content hash=%q want %q", updated.ContentHash, newHash)
		}
	})
	if err := man.Close(); err != nil {
		t.Fatal(err)
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
	registerCoordinatorTestCleanup(t, c)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)
	calls := 0
	embed := &countingEmbedder{mockEmbedder: mockEmbedder{dims: 4}, calls: &calls}
	c.Embed = embed
	if _, err := c.Start(false); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)
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
	registerCoordinatorTestCleanup(t, c)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	if !c.IsRunning() {
		t.Fatal("expected running")
	}
	if _, err := c.Start(true); err == nil {
		t.Fatal("expected already running error")
	} else if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("err=%v want ErrAlreadyRunning", err)
	}
	waitCoordinatorIdle(t, c)
}

type slowEmbedder struct {
	mockEmbedder
	delay time.Duration
}

func (s *slowEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	time.Sleep(s.delay)
	return s.mockEmbedder.Embed(ctx, texts)
}

func TestCoordinatorFileDeletedMidRun(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	slowPath := filepath.Join(root, "pkg", "aaa_slow.go")
	vanishPath := filepath.Join(root, "pkg", "zzz_vanish.go")
	slowContent := "package pkg\n" + strings.Repeat("// line\n", 200)
	if err := os.WriteFile(slowPath, []byte(slowContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vanishPath, []byte("package pkg\n"), 0o644); err != nil {
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
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4, BatchSize: 8}
	store := newMemZvec()
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &slowEmbedder{mockEmbedder: mockEmbedder{dims: 4}, delay: 150 * time.Millisecond}, store, cfg)
	registerCoordinatorTestCleanup(t, c)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)

	if err := os.WriteFile(slowPath, []byte(slowContent+"\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		if err := os.Remove(vanishPath); err != nil {
			t.Error(err)
		}
	}()
	if _, err := c.Start(false); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)

	man, err := manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer man.Close()
	if _, err := man.Get("pkg/zzz_vanish.go"); err == nil {
		t.Fatal("expected vanished file removed from manifest")
	}
	store.mu.Lock()
	for _, ch := range store.chunks {
		if strings.Contains(ch.RelativePath, "zzz_vanish.go") {
			store.mu.Unlock()
			t.Fatalf("unexpected zvec chunk for vanished file: %+v", ch)
		}
	}
	store.mu.Unlock()
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
	registerCoordinatorTestCleanup(t, c)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Start(false); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)
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
	registerCoordinatorTestCleanup(t, c)
	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)
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
	registerCoordinatorTestCleanup(t, c)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)
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
	registerCoordinatorTestCleanup(t, c)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)
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
	if len(p.FailedFiles) != 1 || p.FailedFiles[0] != ".cursor/rules/rule.mdc" {
		t.Fatalf("failed_files=%v", p.FailedFiles)
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

func TestCoordinatorRunStopsOnLifecycleCancel(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		name := filepath.Join(root, "pkg", fmt.Sprintf("file_%d.go", i))
		if err := os.WriteFile(name, []byte("package pkg\n"), 0o644); err != nil {
			t.Fatal(err)
		}
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
				HeartbeatSeconds: 15,
			},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4}
	store := newMemZvec()
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, store, cfg)
	registerCoordinatorTestCleanup(t, c)

	lifeCtx, cancel := context.WithCancel(context.Background())
	c.SetLifecycleContext(lifeCtx)
	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	cancel()

	waitCoordinatorIdle(t, c)
	if c.IsRunning() {
		t.Fatal("expected indexing to stop after lifecycle cancel")
	}
	cur := c.CurrentProgress()
	if cur.State != StateIdle {
		t.Fatalf("state=%q want idle", cur.State)
	}
	if cur.Error != "" {
		t.Fatalf("error=%q want empty after interrupt", cur.Error)
	}
	if cur.Message != InterruptedMessage {
		t.Fatalf("message=%q want %q", cur.Message, InterruptedMessage)
	}
}

func TestCoordinatorWaitForIdle(t *testing.T) {
	c := &Coordinator{}
	if err := c.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := c.WaitForIdle(ctx); err == nil {
		t.Fatal("expected timeout")
	}

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	if err := c.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func newTestCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	settings := &config.Settings{
		WorkspaceRoot: root,
		WorkspaceID:   "test-ws",
		IndexDir:      indexDir,
		App: config.AppConfig{
			Indexing: config.IndexingConfig{
				LockStaleSeconds: 300,
				StallSeconds:     3600,
			},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4}
	store := newMemZvec()
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	return NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, store, cfg)
}

func TestCoordinatorStartAfterClose(t *testing.T) {
	c := newTestCoordinator(t)
	c.Close()
	_, err := c.Start(false)
	if !errors.Is(err, ErrCoordinatorClosed) {
		t.Fatalf("Start after Close: err=%v want ErrCoordinatorClosed", err)
	}
	c.Close()
}

func TestCoordinatorCloseConcurrentStart(t *testing.T) {
	c := newTestCoordinator(t)
	closeDone := make(chan struct{})
	go func() {
		c.Close()
		close(closeDone)
	}()
	for i := 0; i < 50; i++ {
		_, err := c.Start(false)
		switch {
		case errors.Is(err, ErrCoordinatorClosed):
		case err == nil:
			waitCoordinatorIdle(t, c)
		case errors.Is(err, ErrAlreadyRunning):
		}
	}
	<-closeDone
	_, err := c.Start(false)
	if !errors.Is(err, ErrCoordinatorClosed) {
		t.Fatalf("Start after Close completed: err=%v want ErrCoordinatorClosed", err)
	}
}

func TestCoordinatorStartHoldsZvecCloseMu(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.WriteFile(filepath.Join(root, "slow.go"), []byte("package main\n"+strings.Repeat("// x\n", 400)), 0o644); err != nil {
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
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4, BatchSize: 4}
	store := newMemZvec()
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &slowEmbedder{mockEmbedder: mockEmbedder{dims: 4}, delay: 100 * time.Millisecond}, store, cfg)
	registerCoordinatorTestCleanup(t, c)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.IsRunning() {
			if c.TryLockZvecForClose() {
				t.Fatal("TryLockZvecForClose succeeded while indexing is running")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("indexing did not start")
}

func TestManifestStatsNil(t *testing.T) {
	files, chunks, err := manifestStats(nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if files != 0 || chunks != 0 {
		t.Fatalf("files=%d chunks=%d", files, chunks)
	}
}

type failEmbedder struct {
	dims  int
	fail  bool
	calls int
	mu    sync.Mutex
}

func (m *failEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	m.mu.Lock()
	m.calls++
	shouldFail := m.fail
	m.mu.Unlock()
	if shouldFail {
		return nil, fmt.Errorf("embed failed")
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, m.dims)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

func (m *failEmbedder) Dimensions() int { return m.dims }

func TestCoordinatorPreservesIndexOnEmbedFailure(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "pkg", "auth.go")
	if err := os.WriteFile(path, []byte("package pkg\n\nfunc Auth() {}\n"), 0o644); err != nil {
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
	embed := &failEmbedder{dims: 4}
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, embed, store, cfg)
	registerCoordinatorTestCleanup(t, c)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)

	man, err := manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := man.Get("pkg/auth.go")
	if err != nil {
		t.Fatalf("initial manifest: %v", err)
	}
	beforeCount, err := store.DocCount()
	if err != nil || beforeCount == 0 {
		t.Fatalf("initial zvec count=%d err=%v", beforeCount, err)
	}
	_ = man.Close()

	if err := os.WriteFile(path, []byte("package pkg\n\nfunc Auth() {}\nfunc Other() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	embed.fail = true
	if _, err := c.Start(false); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)
	if p := c.CurrentProgress(); p.State != StateError {
		t.Fatalf("expected error state, got %+v", p)
	}

	man, err = manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer man.Close()
	after, err := man.Get("pkg/auth.go")
	if err != nil {
		t.Fatalf("manifest after failed reindex: %v", err)
	}
	if after.MtimeNs != before.MtimeNs || len(after.DocIDs) != len(before.DocIDs) {
		t.Fatalf("manifest changed after embed failure: before=%+v after=%+v", before, after)
	}
	afterCount, err := store.DocCount()
	if err != nil {
		t.Fatal(err)
	}
	if afterCount != beforeCount {
		t.Fatalf("zvec doc count changed: before=%d after=%d", beforeCount, afterCount)
	}
}

func TestStaleDocIDs(t *testing.T) {
	got := staleDocIDs([]string{"a", "b", "c"}, []string{"b", "d"})
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("staleDocIDs=%v", got)
	}
	if stale := staleDocIDs(nil, []string{"a"}); stale != nil {
		t.Fatalf("expected nil, got %v", stale)
	}
}

func TestRollbackNewDocIDs(t *testing.T) {
	old := &manifest.FileEntry{DocIDs: []string{"a", "b", "c"}}
	got := rollbackNewDocIDs([]string{"a", "b", "d"}, old)
	if len(got) != 1 || got[0] != "d" {
		t.Fatalf("rollbackNewDocIDs=%v", got)
	}
	if got := rollbackNewDocIDs([]string{"x", "y"}, nil); len(got) != 2 {
		t.Fatalf("rollback without old=%v", got)
	}
}

type lockErrorZvec struct {
	memZvec
	openErr error
}

func (s *lockErrorZvec) Open() error {
	return s.openErr
}

func (s *lockErrorZvec) IsOpen() bool {
	return false
}

func (s *lockErrorZvec) DocCount() (int, error) {
	return 0, s.openErr
}

func TestManifestZvecDesyncLockError(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manPath := filepath.Join(indexDir, "manifest.db")
	man, err := manifest.Open(manPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := man.Upsert(manifest.FileEntry{
		RelativePath: "pkg/auth.go",
		MtimeNs:      1,
		Size:         10,
		ChunkCount:   2,
		DocIDs:       []string{"d1", "d2"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := man.Close(); err != nil {
		t.Fatal(err)
	}

	lockErr := errors.New(`zvec error [INTERNAL_ERROR]: Can't lock read-only collection: /tmp/ws/LOCK`)
	settings := &config.Settings{
		WorkspaceRoot: root,
		IndexDir:      indexDir,
	}
	c := &Coordinator{
		Settings: settings,
		Zvec:     &lockErrorZvec{openErr: lockErr},
	}
	need, err := c.manifestZvecDesync()
	if err != nil {
		t.Fatalf("manifestZvecDesync: %v", err)
	}
	if !need {
		t.Fatal("expected force reindex when zvec lock fails but manifest has chunks")
	}
}

func (s *lockErrorZvec) DocIDsPresent([]string) (bool, error) {
	return false, s.openErr
}

func TestManifestZvecDesyncMissingManifestDoc(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manPath := filepath.Join(indexDir, "manifest.db")
	man, err := manifest.Open(manPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := man.Upsert(manifest.FileEntry{
		RelativePath: "a.go",
		MtimeNs:      1,
		Size:         10,
		ChunkCount:   2,
		DocIDs:       []string{"d1", "d2"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := man.Close(); err != nil {
		t.Fatal(err)
	}

	store := newMemZvec()
	store.chunks["d1"] = zvec.Chunk{DocID: "d1"}

	c := &Coordinator{
		Settings: &config.Settings{IndexDir: indexDir},
		Zvec:     store,
	}
	need, err := c.manifestZvecDesync()
	if err != nil {
		t.Fatalf("manifestZvecDesync: %v", err)
	}
	if !need {
		t.Fatal("expected desync when manifest doc missing from zvec")
	}
}

func TestManifestZvecDesyncSameCountDifferentSets(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manPath := filepath.Join(indexDir, "manifest.db")
	man, err := manifest.Open(manPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := man.Upsert(manifest.FileEntry{
		RelativePath: "a.go",
		MtimeNs:      1,
		Size:         10,
		ChunkCount:   3,
		DocIDs:       []string{"d1", "d2", "d3"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := man.Close(); err != nil {
		t.Fatal(err)
	}

	store := newMemZvec()
	store.chunks["d1"] = zvec.Chunk{DocID: "d1"}
	store.chunks["d2"] = zvec.Chunk{DocID: "d2"}
	store.chunks["orphan"] = zvec.Chunk{DocID: "orphan"}

	c := &Coordinator{
		Settings: &config.Settings{IndexDir: indexDir},
		Zvec:     store,
	}
	need, err := c.manifestZvecDesync()
	if err != nil {
		t.Fatalf("manifestZvecDesync: %v", err)
	}
	if !need {
		t.Fatal("expected desync when counts match but doc id sets differ")
	}
}

type batchFailEmbedder struct {
	dims      int
	failAfter int
	calls     int
}

func (m *batchFailEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	m.calls++
	if m.calls >= m.failAfter {
		return nil, fmt.Errorf("embed failed on batch %d", m.calls)
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, m.dims)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

func (m *batchFailEmbedder) Dimensions() int { return m.dims }

func TestCoordinatorPreservesIndexOnSecondBatchEmbedFailure(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "pkg", "big.go")
	var b strings.Builder
	b.WriteString("package pkg\n\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "func Fn%d() {}\n\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
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
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4, BatchSize: 1}
	store := newMemZvec()
	embed := &batchFailEmbedder{dims: 4, failAfter: 1000}
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, embed, store, cfg)
	registerCoordinatorTestCleanup(t, c)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)

	man, err := manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := man.Get("pkg/big.go")
	if err != nil {
		t.Fatalf("initial manifest: %v", err)
	}
	_ = man.Close()

	if err := os.WriteFile(path, []byte(b.String()+"\nfunc Tail() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	embed.failAfter = 2
	embed.calls = 0

	if _, err := c.Start(false); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)

	man, err = manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer man.Close()
	after, err := man.Get("pkg/big.go")
	if err != nil {
		t.Fatalf("manifest after failed reindex: %v", err)
	}
	if after.MtimeNs != before.MtimeNs || len(after.DocIDs) != len(before.DocIDs) {
		t.Fatalf("manifest changed after second-batch embed failure: before=%+v after=%+v", before, after)
	}

	count, err := store.DocCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != len(before.DocIDs) {
		t.Fatalf("doc count after embed failure=%d want %d", count, len(before.DocIDs))
	}
	present, err := store.DocIDsPresent(before.DocIDs)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatalf("manifest doc ids missing from zvec after embed failure: %v", before.DocIDs)
	}
}

type partialUpsertZvec struct {
	memZvec
}

func (s *partialUpsertZvec) UpsertChunks(chunks []zvec.Chunk, vectors [][]float32) error {
	if len(chunks) == 0 {
		return nil
	}
	if err := s.memZvec.UpsertChunks(chunks[:1], vectors[:1]); err != nil {
		return err
	}
	return &zvec.PartialWriteError{
		Op:        "upsert",
		Succeeded: []string{chunks[0].DocID},
		Failed:    1,
		Cause:     fmt.Errorf("partial upsert"),
	}
}

func TestCoordinatorPartialUpsertDoesNotCommitManifest(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "pkg", "auth.go")
	if err := os.WriteFile(path, []byte("package pkg\n\nfunc Auth() {}\nfunc Other() {}\n"), 0o644); err != nil {
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
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4, BatchSize: 1}
	store := &partialUpsertZvec{memZvec: *newMemZvec()}
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, store, cfg)
	registerCoordinatorTestCleanup(t, c)

	if _, err := c.Start(true); err != nil {
		t.Fatal(err)
	}
	waitCoordinatorIdle(t, c)
	if p := c.CurrentProgress(); p.State != StateError {
		t.Fatalf("expected error state, got %+v", p)
	}

	man, err := manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer man.Close()
	if _, err := man.Get("pkg/auth.go"); err == nil {
		t.Fatal("expected no manifest row after partial upsert failure")
	}
	if len(store.chunks) != 0 {
		t.Fatalf("expected zvec rollback, got %d docs", len(store.chunks))
	}
}
