package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

type mockZvecStore struct {
	mu         sync.Mutex
	hits       []zvec.SearchHit
	err        error
	openErr    error
	wipeErr    error
	wipeCalled bool
	open       bool
	closeCalls int
}

func (m *mockZvecStore) Open() error {
	if m.openErr != nil {
		return m.openErr
	}
	m.open = true
	return nil
}
func (m *mockZvecStore) IsOpen() bool { return m.open }
func (m *mockZvecStore) Close() error {
	m.closeCalls++
	m.open = false
	return nil
}
func (m *mockZvecStore) DocCount() (int, error)                       { return len(m.hits), nil }
func (m *mockZvecStore) UpsertChunks([]zvec.Chunk, [][]float32) error { return nil }
func (m *mockZvecStore) DeleteByIDs([]string) error                   { return nil }
func (m *mockZvecStore) WipeCollection() error {
	m.mu.Lock()
	m.wipeCalled = true
	m.mu.Unlock()
	return m.wipeErr
}

func (m *mockZvecStore) wasWipeCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.wipeCalled
}
func (m *mockZvecStore) Search([]float32, int, string) ([]zvec.SearchHit, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.hits, nil
}

type recoveringZvecStore struct {
	mockZvecStore
	opens int
}

func (r *recoveringZvecStore) Open() error {
	r.opens++
	if r.opens == 1 {
		return errors.New(`Can't open lock file: test lock`)
	}
	return r.openErr
}

type lockFailZvecStore struct{ mockZvecStore }

func (s *lockFailZvecStore) Open() error {
	return errors.New(`Can't open lock file: test lock`)
}

type searchLockZvecStore struct {
	mockZvecStore
	attempts int
}

func (s *searchLockZvecStore) Search([]float32, int, string) ([]zvec.SearchHit, error) {
	s.attempts++
	if s.attempts == 1 {
		return nil, errors.New(`Can't open lock file: test lock`)
	}
	return []zvec.SearchHit{{Path: "pkg/auth.go", Score: 0.9, Snippet: "func Auth()"}}, nil
}

func insertManifestRow(t *testing.T, indexDir, path string, chunks int) error {
	t.Helper()
	store, err := manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	db, err := sql.Open("sqlite", filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`
		INSERT INTO file_manifest (relative_path, mtime_ns, size, chunk_count, doc_ids)
		VALUES (?, ?, ?, ?, ?)
	`, path, 1, 10, chunks, `[]`)
	return err
}

func waitCoordinatorIdle(t *testing.T, p *Phase1) {
	t.Helper()
	if p.coordinator == nil {
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !p.coordinator.IsRunning() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("coordinator still running after timeout")
}

func releasePhase1TestResources(t *testing.T, p *Phase1) {
	t.Helper()
	if p == nil {
		return
	}
	waitCoordinatorIdle(t, p)
	if p.coordinator != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = p.coordinator.WaitForIdle(ctx)
		cancel()
		_ = p.coordinator.ReleaseLock()
	}
	if p.zvec != nil {
		_ = p.zvec.Close()
	}
}

func modelsEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusOK)
		case "/v1/embeddings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"index": 0, "embedding": []float64{0.1, 0.2, 0.3}},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func phase1Settings(t *testing.T, embedURL string) *config.Settings {
	t.Helper()
	dir := t.TempDir()
	return &config.Settings{
		WorkspaceRoot: dir,
		IndexDir:      filepath.Join(dir, "index"),
		ConfigPath:    filepath.Join(dir, "config.yaml"),
		GitHubRepo:    "test/repo",
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {
					Provider:   "openai_compatible",
					Model:      "test-model",
					Dimensions: 3,
					BaseURL:    embedURL,
				},
			},
			Indexing: config.IndexingConfig{
				Chunking: config.ChunkingConfig{
					Strategy: "hybrid",
					Version:  1,
				},
			},
		},
	}
}

func TestNewPhase1(t *testing.T) {
	t.Run("with embed profile", func(t *testing.T) {
		p, err := NewPhase1(phase1Settings(t, "http://127.0.0.1:9/v1"))
		if err != nil {
			t.Fatalf("NewPhase1: %v", err)
		}
		if p.embed == nil {
			t.Fatal("expected embed client")
		}
	})

	t.Run("missing active profile", func(t *testing.T) {
		settings := phase1Settings(t, "http://127.0.0.1:9/v1")
		settings.App.ActiveProfile = ""
		_, err := NewPhase1(settings)
		if err == nil {
			t.Fatal("expected error for missing active profile")
		}
	})

	t.Run("invalid embed profile", func(t *testing.T) {
		settings := phase1Settings(t, "")
		_, err := NewPhase1(settings)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPhase1GetIndexStatus(t *testing.T) {
	p, err := NewPhase1(phase1Settings(t, "http://127.0.0.1:9/v1"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatalf("GetIndexStatus: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["server_version"] != version.Version {
		t.Fatalf("server_version=%v", payload["server_version"])
	}
	if payload["index_dir"] != "index" {
		t.Fatalf("index_dir=%v", payload["index_dir"])
	}
	if payload["chunking_strategy"] != "hybrid" {
		t.Fatalf("chunking_strategy=%v", payload["chunking_strategy"])
	}
	if payload["chunking_version"] != float64(1) {
		t.Fatalf("chunking_version=%v", payload["chunking_version"])
	}
}

func TestPhase1SemanticSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 0, "embedding": []float64{0.1, 0.2, 0.3}},
			},
		})
	}))
	defer srv.Close()

	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}

	raw, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "hello"})
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["query"] != "hello" {
		t.Fatalf("query=%v", payload["query"])
	}
	if _, ok := payload["message"]; !ok {
		t.Fatalf("expected zvec stub message, payload=%v", payload)
	}
}

func TestSemanticSearchContextCanceled(t *testing.T) {
	p, err := NewPhase1(phase1Settings(t, "http://127.0.0.1:9/v1"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.SemanticSearch(ctx, SearchRequest{Query: "hello"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestPhase1SemanticSearchWithMockZvec(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 0, "embedding": []float64{0.1, 0.2, 0.3}},
			},
		})
	}))
	defer srv.Close()

	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	p.zvec = &mockZvecStore{
		hits: []zvec.SearchHit{{
			Path:      "internal/auth.go",
			StartLine: 1,
			EndLine:   10,
			Score:     0.95,
			Snippet:   "auth middleware",
		}},
	}

	raw, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "auth"})
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	results, ok := payload["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results=%v", payload["results"])
	}
	if perf, ok := payload["performance"].(map[string]any); !ok || perf["degraded"] == nil || perf["total_ms"] == nil {
		t.Fatalf("missing performance: %v", payload["performance"])
	}
}

func TestPhase1SemanticSearchNoEmbed(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	p := &Phase1{
		Settings:    settings,
		zvec:        zvec.New(zvec.Config{IndexDir: settings.IndexDir, WorkspaceRoot: settings.WorkspaceRoot}),
		searchStats: NewSearchStats(settings.App.Search),
	}
	raw, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "hello"})
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["message"]; !ok {
		t.Fatalf("payload=%v", payload)
	}
}

func TestPhase1SemanticSearchEmbedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "hello"})
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["message"]; !ok {
		t.Fatalf("payload=%v", payload)
	}
}

func TestPhase1ReindexCheckUpdateReady(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitCoordinatorIdle(t, p) })
	p.zvec = &mockZvecStore{}
	if err := os.MkdirAll(p.Settings.IndexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.EnsureIndexMeta(p.Settings.IndexDir, zvec.IndexIdentity{
		WorkspaceID:      p.Settings.WorkspaceID,
		WorkspaceRoot:    p.Settings.WorkspaceRoot,
		Profile:          p.Settings.App.ActiveProfile,
		Dimensions:       3,
		ChunkingVersion:  p.Settings.App.Indexing.Chunking.Version,
		ChunkingStrategy: p.Settings.App.Indexing.Chunking.Strategy,
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if _, err := p.Reindex(context.Background(), ReindexRequest{Force: true}); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	raw, err := p.CheckUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["installed_version"] == nil {
		t.Fatalf("payload=%v", payload)
	}
}

func TestDerefString(t *testing.T) {
	if derefString(nil) != "" {
		t.Fatal("nil")
	}
	s := "x"
	if derefString(&s) != "x" {
		t.Fatal("value")
	}
}

func TestPhase1ManifestStats(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	if err := os.MkdirAll(settings.IndexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := insertManifestRow(t, settings.IndexDir, "a.go", 3); err != nil {
		t.Fatal(err)
	}

	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	files, chunks := p.manifestStats()
	if files != 1 || chunks != 3 {
		t.Fatalf("files=%d chunks=%d", files, chunks)
	}
}

func TestPhase1ManifestStatsMissingDB(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	files, chunks := p.manifestStats()
	if files != 0 || chunks != 0 {
		t.Fatalf("files=%d chunks=%d", files, chunks)
	}
}

func TestPhase1GetIndexStatusWithManifest(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	if err := os.MkdirAll(settings.IndexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := insertManifestRow(t, settings.IndexDir, "b.go", 2); err != nil {
		t.Fatal(err)
	}

	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["indexed_files"] != float64(1) {
		t.Fatalf("indexed_files=%v", payload["indexed_files"])
	}
}

func TestSemanticSearchWhileIndexing(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitCoordinatorIdle(t, p) })
	p.zvec = &mockZvecStore{hits: []zvec.SearchHit{{Path: "internal/auth.go", Score: 0.9, Snippet: "auth"}}}
	if _, err := p.Reindex(context.Background(), ReindexRequest{Force: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "auth"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	idx, _ := payload["indexing"].(map[string]any)
	if idx == nil || idx["running"] != true {
		t.Fatalf("payload=%v", payload)
	}
	results, _ := payload["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results=%v", payload["results"])
	}
	if payload["message"] != "results may be incomplete while indexing is in progress" {
		t.Fatalf("message=%v", payload["message"])
	}
}

func TestReindexNoCoordinator(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	p := &Phase1{
		Settings:    settings,
		zvec:        zvec.New(zvec.Config{IndexDir: settings.IndexDir, WorkspaceRoot: settings.WorkspaceRoot}),
		searchStats: NewSearchStats(settings.App.Search),
	}
	raw, err := p.Reindex(context.Background(), ReindexRequest{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["started"] != false {
		t.Fatalf("payload=%v", payload)
	}
}

func TestReadyWhenIndexing(t *testing.T) {
	p, err := NewPhase1(phase1Settings(t, "http://127.0.0.1:9/v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitCoordinatorIdle(t, p) })
	p.zvec = &mockZvecStore{}
	if _, err := p.Reindex(context.Background(), ReindexRequest{Force: true}); err != nil {
		t.Fatal(err)
	}
	if err := p.Ready(context.Background()); err == nil {
		t.Fatal("expected indexing in progress error")
	}
}

func TestGetIndexStatusSearchPerformance(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	p.zvec = &mockZvecStore{hits: []zvec.SearchHit{{Path: "a.go", Score: 1, Snippet: "x"}}}
	if _, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "hello"}); err != nil {
		t.Fatal(err)
	}
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	perf, _ := payload["search_performance"].(map[string]any)
	if perf["samples"] != float64(1) {
		t.Fatalf("search_performance=%v", perf)
	}
}

func TestStartFileWatcherEnabled(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	settings.App.FileWatcher = config.FileWatcherConfig{
		Enabled:             true,
		Backend:             "polling",
		DebounceSeconds:     0.05,
		PollIntervalSeconds: 0.1,
	}
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.StartFileWatcher(ctx)
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	fw, ok := payload["file_watcher"].(map[string]any)
	if !ok {
		t.Fatalf("file_watcher=%v", payload["file_watcher"])
	}
	if fw["backend"] != "polling" {
		t.Fatalf("file_watcher=%v", fw)
	}
	if fw["enabled_in_config"] != true {
		t.Fatalf("file_watcher=%v", fw)
	}
}

func TestStartFileWatcherDisabled(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	settings.App.FileWatcher.Enabled = false
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.StartFileWatcher(ctx)
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	fw, _ := payload["file_watcher"].(map[string]any)
	if fw["running"] == true {
		t.Fatalf("file_watcher=%v", fw)
	}
}

func TestSemanticSearchZvecGenericError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []float64{0.1, 0.2, 0.3}}},
		})
	}))
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	p.zvec = &mockZvecStore{err: errors.New("search failed")}
	raw, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["message"] == nil {
		t.Fatalf("payload=%v", payload)
	}
}

func TestSemanticSearchIndexingWithoutCoordinator(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	if err := os.MkdirAll(settings.IndexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := indexer.NewProgressStore(settings.IndexDir).Save(indexer.StartRunning(false)); err != nil {
		t.Fatal(err)
	}
	p := &Phase1{
		Settings:    settings,
		zvec:        zvec.New(zvec.Config{IndexDir: settings.IndexDir, WorkspaceRoot: settings.WorkspaceRoot}),
		searchStats: NewSearchStats(settings.App.Search),
	}
	raw, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "auth"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["indexing"]; ok {
		t.Fatalf("expected no indexing block without successful search: %v", payload)
	}
	if payload["message"] == nil {
		t.Fatalf("payload=%v", payload)
	}
}

func TestReindexAlreadyRunning(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitCoordinatorIdle(t, p) })
	p.zvec = &mockZvecStore{}
	if _, err := p.Reindex(context.Background(), ReindexRequest{Force: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := p.Reindex(context.Background(), ReindexRequest{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["started"] != false {
		t.Fatalf("payload=%v", payload)
	}
}

func TestStartAutoIndexDisabled(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	settings.AutoIndexOnStart = false
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	p.StartAutoIndex()
	if p.coordinator.IsRunning() {
		t.Fatal("expected no auto index")
	}
}

func TestGetIndexStatusProgressWithoutCoordinator(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	if err := os.MkdirAll(settings.IndexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := indexer.NewProgressStore(settings.IndexDir)
	if err := store.Save(indexer.StartRunning(false)); err != nil {
		t.Fatal(err)
	}
	p := &Phase1{
		Settings:    settings,
		zvec:        zvec.New(zvec.Config{IndexDir: settings.IndexDir, WorkspaceRoot: settings.WorkspaceRoot}),
		searchStats: NewSearchStats(settings.App.Search),
	}
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	idx, _ := payload["indexing"].(map[string]any)
	if idx["running"] != true {
		t.Fatalf("indexing=%v", idx)
	}
}

func TestSemanticSearchPathGlob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []float64{0.1, 0.2, 0.3}}},
		})
	}))
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	glob := "*.go"
	p.zvec = &mockZvecStore{hits: []zvec.SearchHit{{Path: "a.go", Score: 1, Snippet: "x"}}}
	if _, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "x", PathGlob: &glob}); err != nil {
		t.Fatal(err)
	}
}

func TestStartAutoIndex(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	settings.AutoIndexOnStart = true
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitCoordinatorIdle(t, p) })
	p.StartAutoIndex()
	if p.coordinator == nil {
		t.Fatal("expected coordinator")
	}
}

func TestReadyNoIndexMeta(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Ready(context.Background()); err == nil || err.Error() != "index not built yet" {
		t.Fatalf("Ready: %v", err)
	}
}

func TestReadyEmbeddingsUnreachable(t *testing.T) {
	p, err := NewPhase1(phase1Settings(t, "http://127.0.0.1:9/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.Settings.IndexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.EnsureIndexMeta(p.Settings.IndexDir, zvec.IndexIdentity{
		WorkspaceID:      p.Settings.WorkspaceID,
		WorkspaceRoot:    p.Settings.WorkspaceRoot,
		Profile:          p.Settings.App.ActiveProfile,
		Dimensions:       3,
		ChunkingVersion:  p.Settings.App.Indexing.Chunking.Version,
		ChunkingStrategy: p.Settings.App.Indexing.Chunking.Strategy,
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "embeddings unreachable") {
		t.Fatalf("Ready: %v", err)
	}
}

func TestReadyMissingCollection(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.Settings.IndexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.EnsureIndexMeta(p.Settings.IndexDir, zvec.IndexIdentity{
		WorkspaceID:      p.Settings.WorkspaceID,
		WorkspaceRoot:    p.Settings.WorkspaceRoot,
		Profile:          p.Settings.App.ActiveProfile,
		Dimensions:       3,
		ChunkingVersion:  p.Settings.App.Indexing.Chunking.Version,
		ChunkingStrategy: p.Settings.App.Indexing.Chunking.Strategy,
	}); err != nil {
		t.Fatal(err)
	}
	p.zvec = &mockZvecStore{openErr: zvec.ErrCollectionMissing}
	if err := p.Ready(context.Background()); err == nil || err.Error() != "index not built yet" {
		t.Fatalf("Ready: %v", err)
	}
}

func TestPhase1Close(t *testing.T) {
	store := &mockZvecStore{}
	p := &Phase1{zvec: store, zvecCfg: zvec.Config{}}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if store.closeCalls != 1 {
		t.Fatalf("closeCalls=%d want 1", store.closeCalls)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if store.closeCalls != 1 {
		t.Fatalf("closeCalls=%d want 1 after second Close", store.closeCalls)
	}
	if err := (&Phase1{}).Close(); err != nil {
		t.Fatal(err)
	}
}

type phase1StubEmbedder struct {
	dims int
}

func (e *phase1StubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, e.dims)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

func (e *phase1StubEmbedder) Dimensions() int { return e.dims }

func phase1MigrationSettings(t *testing.T, autoIndex bool) (*config.Settings, string) {
	t.Helper()
	dir := t.TempDir()
	indexDir := filepath.Join(dir, "index")
	return &config.Settings{
		WorkspaceRoot:    dir,
		WorkspaceID:      "ws1",
		IndexDir:         indexDir,
		AutoIndexOnStart: autoIndex,
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible", Dimensions: 3},
			},
			Indexing: config.IndexingConfig{
				Extensions:       []string{".go"},
				LockStaleSeconds: 300,
				StallSeconds:     120,
				Chunking: config.ChunkingConfig{
					Strategy: "hybrid",
					Version:  1,
				},
			},
		},
	}, indexDir
}

func TestPrepareStartupSkipsWhenVersionsMatch(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, true)
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.EnsureIndexMeta(indexDir, zvec.IndexIdentity{
		WorkspaceID:      settings.WorkspaceID,
		WorkspaceRoot:    settings.WorkspaceRoot,
		Profile:          settings.App.ActiveProfile,
		Dimensions:       3,
		ChunkingVersion:  settings.App.Indexing.Chunking.Version,
		ChunkingStrategy: settings.App.Indexing.Chunking.Strategy,
	}); err != nil {
		t.Fatal(err)
	}

	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 3}
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 3}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
	}
	p.PrepareStartup()
	if store.wasWipeCalled() {
		t.Fatal("unexpected wipe")
	}
	waitCoordinatorIdle(t, p)
}

func TestGetIndexStatusZvecGoVersions(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, false)
	settings.App.Profiles = map[string]config.EmbeddingProfile{
		"test": {Provider: "openai_compatible", Model: "m", Dimensions: 3},
	}
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{ZvecGoVersion: "v0.3.1"}); err != nil {
		t.Fatal(err)
	}
	p := &Phase1{
		Settings:    settings,
		zvec:        &mockZvecStore{},
		searchStats: NewSearchStats(settings.App.Search),
		zvecCfg: zvec.Config{
			IndexDir:      indexDir,
			WorkspaceRoot: settings.WorkspaceRoot,
			ProfileName:   settings.App.ActiveProfile,
			Dimensions:    3,
		},
	}
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["zvec_go_version"] != version.ZvecGoVersion {
		t.Fatalf("zvec_go_version=%v", payload["zvec_go_version"])
	}
	if payload["index_zvec_go_version"] != "v0.3.1" {
		t.Fatalf("index_zvec_go_version=%v", payload["index_zvec_go_version"])
	}
}

func TestPrepareStartupMigratesWithAutoIndex(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, true)
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{ZvecGoVersion: "v0.3.1"}); err != nil {
		t.Fatal(err)
	}

	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 3}
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 3}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
	}
	p.PrepareStartup()

	if !store.wasWipeCalled() {
		t.Fatal("expected wipe during migration")
	}
	meta, err := zvec.ReadIndexMeta(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ZvecGoVersion != version.ZvecGoVersion || meta.WorkspaceID != "ws1" {
		t.Fatalf("meta=%+v", meta)
	}
	waitCoordinatorIdle(t, p)
}

func TestPrepareStartupMigratesManifestOnly(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, false)
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manStore, err := manifest.Open(filepath.Join(indexDir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := manStore.Close(); err != nil {
		t.Fatal(err)
	}

	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 3}
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 3}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
	}
	p.PrepareStartup()

	meta, err := zvec.ReadIndexMeta(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	wantCollection := zvec.CollectionName(settings.WorkspaceRoot, settings.App.ActiveProfile, profile.Dimensions)
	if meta.WorkspaceID != settings.WorkspaceID ||
		meta.WorkspaceRoot != settings.WorkspaceRoot ||
		meta.CollectionName != wantCollection ||
		meta.ZvecGoVersion != version.ZvecGoVersion {
		t.Fatalf("meta=%+v", meta)
	}
	if coord.IsRunning() {
		t.Fatal("expected no background reindex when AUTO_INDEX_ON_START=false")
	}
}

func TestPrepareStartupMigratesWithoutAutoIndex(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, false)
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{ZvecGoVersion: "v0.3.1"}); err != nil {
		t.Fatal(err)
	}

	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 3}
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 3}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
	}
	p.PrepareStartup()

	if !store.wasWipeCalled() {
		t.Fatal("expected wipe during migration")
	}
	meta, err := zvec.ReadIndexMeta(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ZvecGoVersion != version.ZvecGoVersion || meta.WorkspaceID != "ws1" {
		t.Fatalf("meta=%+v", meta)
	}
	if coord.IsRunning() {
		t.Fatal("expected no background reindex when AUTO_INDEX_ON_START=false")
	}
	if p.startupMsg == "" {
		t.Fatal("expected startup message")
	}
}

func TestPrepareStartupChunkingMismatchAutoIndex(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, true)
	settings.App.Profiles = map[string]config.EmbeddingProfile{
		"test": {Provider: "openai_compatible", Model: "m", Dimensions: 3},
	}
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{
		WorkspaceID:         settings.WorkspaceID,
		WorkspaceRoot:       settings.WorkspaceRoot,
		EmbeddingProfile:    settings.App.ActiveProfile,
		EmbeddingDimensions: 3,
		CollectionName:      zvec.CollectionName(settings.WorkspaceRoot, settings.App.ActiveProfile, 3),
		ZvecGoVersion:       version.ZvecGoVersion,
		ChunkingVersion:     0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings.WorkspaceRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 3}
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 3}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
	}
	p.PrepareStartup()

	t.Cleanup(func() { releasePhase1TestResources(t, p) })

	waitCoordinatorIdle(t, p)
	if !store.wasWipeCalled() {
		t.Fatal("expected wipe during chunking identity migration")
	}
	meta, err := zvec.ReadIndexMeta(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ChunkingVersion != settings.App.Indexing.Chunking.Version {
		t.Fatalf("ChunkingVersion=%d want %d", meta.ChunkingVersion, settings.App.Indexing.Chunking.Version)
	}
	if meta.ChunkingStrategy != settings.App.Indexing.Chunking.Strategy {
		t.Fatalf("ChunkingStrategy=%q want %q", meta.ChunkingStrategy, settings.App.Indexing.Chunking.Strategy)
	}
}

func TestReindexChunkingMismatchWithoutForce(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, false)
	settings.App.Profiles = map[string]config.EmbeddingProfile{
		"test": {Provider: "openai_compatible", Model: "m", Dimensions: 3, BaseURL: "http://127.0.0.1:9/v1"},
	}
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{
		WorkspaceID:         settings.WorkspaceID,
		WorkspaceRoot:       settings.WorkspaceRoot,
		EmbeddingProfile:    settings.App.ActiveProfile,
		EmbeddingDimensions: 3,
		CollectionName:      zvec.CollectionName(settings.WorkspaceRoot, settings.App.ActiveProfile, 3),
		ZvecGoVersion:       version.ZvecGoVersion,
		ChunkingVersion:     0,
	}); err != nil {
		t.Fatal(err)
	}

	profile := settings.App.Profiles["test"]
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 3}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
	}

	raw, err := p.Reindex(context.Background(), ReindexRequest{Force: false})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["started"] != false {
		t.Fatalf("payload=%v", payload)
	}
	msg, _ := payload["message"].(string)
	if !strings.Contains(msg, "chunking_version mismatch") {
		t.Fatalf("message=%q", msg)
	}
	if coord.IsRunning() {
		t.Fatal("expected no indexing without force")
	}
}

func TestReindexChunkingMismatchWithForce(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, false)
	settings.App.Profiles = map[string]config.EmbeddingProfile{
		"test": {Provider: "openai_compatible", Model: "m", Dimensions: 3},
	}
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{
		WorkspaceID:         settings.WorkspaceID,
		WorkspaceRoot:       settings.WorkspaceRoot,
		EmbeddingProfile:    settings.App.ActiveProfile,
		EmbeddingDimensions: 3,
		CollectionName:      zvec.CollectionName(settings.WorkspaceRoot, settings.App.ActiveProfile, 3),
		ZvecGoVersion:       version.ZvecGoVersion,
		ChunkingVersion:     0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings.WorkspaceRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	profile := settings.App.Profiles["test"]
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 3}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
	}
	t.Cleanup(func() { releasePhase1TestResources(t, p) })

	raw, err := p.Reindex(context.Background(), ReindexRequest{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["started"] != true {
		t.Fatalf("payload=%v", payload)
	}
	waitCoordinatorIdle(t, p)
	meta, err := zvec.ReadIndexMeta(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ChunkingVersion != settings.App.Indexing.Chunking.Version {
		t.Fatalf("ChunkingVersion=%d want %d", meta.ChunkingVersion, settings.App.Indexing.Chunking.Version)
	}
}

func TestPrepareStartupOwnerMismatchAutoIndex(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, true)
	settings.App.Profiles = map[string]config.EmbeddingProfile{
		"test": {Provider: "openai_compatible", Model: "m", Dimensions: 3},
	}
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{
		WorkspaceID:         "ws-old",
		WorkspaceRoot:       filepath.Join(settings.WorkspaceRoot, "old"),
		EmbeddingProfile:    settings.App.ActiveProfile,
		EmbeddingDimensions: 3,
		ZvecGoVersion:       version.ZvecGoVersion,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings.WorkspaceRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 3}
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 3}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
	}
	p.PrepareStartup()

	t.Cleanup(func() { releasePhase1TestResources(t, p) })

	waitCoordinatorIdle(t, p)
	if !store.wasWipeCalled() {
		t.Fatal("expected wipe during identity migration")
	}
	meta, err := zvec.ReadIndexMeta(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.WorkspaceID != settings.WorkspaceID {
		t.Fatalf("meta=%+v", meta)
	}
}

func TestPrepareStartupOwnerMismatchNoAutoIndex(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, false)
	settings.App.Profiles = map[string]config.EmbeddingProfile{
		"test": {Provider: "openai_compatible", Model: "m", Dimensions: 3},
	}
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{
		WorkspaceID:         "ws-old",
		EmbeddingProfile:    settings.App.ActiveProfile,
		EmbeddingDimensions: 3,
		ZvecGoVersion:       version.ZvecGoVersion,
	}); err != nil {
		t.Fatal(err)
	}

	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 3}
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 3}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
	}
	p.PrepareStartup()

	if coord.IsRunning() {
		t.Fatal("expected no background reindex when AUTO_INDEX_ON_START=false")
	}
	if p.startupMsg == "" {
		t.Fatal("expected startup message")
	}
}

func TestPrepareStartupProfileMismatchNoAutoIndex(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, false)
	settings.App.ActiveProfile = "profile_b"
	settings.App.Profiles = map[string]config.EmbeddingProfile{
		"profile_a": {Provider: "openai_compatible", Model: "m", Dimensions: 1024},
		"profile_b": {Provider: "openai_compatible", Model: "m", Dimensions: 1024},
	}
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{
		WorkspaceID:         settings.WorkspaceID,
		EmbeddingProfile:    "profile_a",
		EmbeddingDimensions: 1024,
		ZvecGoVersion:       version.ZvecGoVersion,
	}); err != nil {
		t.Fatal(err)
	}

	profile := settings.App.Profiles["profile_b"]
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 1024}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
		searchStats: NewSearchStats(settings.App.Search),
	}
	p.PrepareStartup()

	if strings.Contains(p.startupMsg, "workspace path changed") {
		t.Fatalf("misleading message: %q", p.startupMsg)
	}
	if !strings.Contains(p.startupMsg, "profile mismatch") {
		t.Fatalf("message=%q", p.startupMsg)
	}

	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["identity_mismatch"] != true {
		t.Fatalf("identity_mismatch=%v", payload["identity_mismatch"])
	}
	reason, _ := payload["identity_mismatch_reason"].(string)
	if !strings.Contains(reason, "profile mismatch") {
		t.Fatalf("identity_mismatch_reason=%q", reason)
	}
}

func TestGetIndexStatusStartupMsg(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, false)
	settings.App.Profiles = map[string]config.EmbeddingProfile{
		"test": {Provider: "openai_compatible", Model: "m", Dimensions: 3},
	}
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{ZvecGoVersion: "v0.3.1"}); err != nil {
		t.Fatal(err)
	}

	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 3}
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 3}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
		searchStats: NewSearchStats(settings.App.Search),
	}
	p.PrepareStartup()

	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["message"] != p.startupMsg {
		t.Fatalf("message=%v", payload["message"])
	}
}

func TestPrepareStartupNilCoordinator(t *testing.T) {
	settings, _ := phase1MigrationSettings(t, true)
	p := &Phase1{Settings: settings}
	p.PrepareStartup()
}

func TestPrepareStartupMigrationCheckError(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, true)
	settings.App.Profiles = nil
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "index_meta.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 3}
	store := &mockZvecStore{}
	zcfg := zvec.Config{
		IndexDir:      indexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, &phase1StubEmbedder{dims: 3}, store, zcfg)
	p := &Phase1{
		Settings:    settings,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
	}
	p.PrepareStartup()
	if store.wasWipeCalled() {
		t.Fatal("unexpected wipe on corrupt meta")
	}
	waitCoordinatorIdle(t, p)
}

func TestPhase1SetLifecycleContext(t *testing.T) {
	settings := phase1Settings(t, modelsEmbedServer(t).URL)
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.SetLifecycleContext(ctx)
	if p.coordinator == nil {
		t.Fatal("expected coordinator")
	}
}

func TestPhase1StartFileWatcherNilCoordinator(t *testing.T) {
	p := &Phase1{Settings: phase1Settings(t, "")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.StartFileWatcher(ctx)
	if p.watcherInst != nil {
		t.Fatal("expected no watcher without coordinator")
	}
}

func TestPhase1StartFileWatcher(t *testing.T) {
	settings := phase1Settings(t, modelsEmbedServer(t).URL)
	settings.App.FileWatcher.Enabled = true
	settings.App.FileWatcher.Backend = "polling"
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.StartFileWatcher(ctx)
	if p.watcherInst == nil {
		t.Fatal("expected watcher instance")
	}
}

func TestPhase1GetIndexStatusRecoversZvecLock(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	store := &recoveringZvecStore{}
	p.zvec = store
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["zvec_open_ok"] != true {
		t.Fatalf("payload=%v", payload)
	}
	if store.opens < 2 {
		t.Fatalf("opens=%d, want recovery retry", store.opens)
	}
}

func TestPhase1GetIndexStatusLockErrorDiagnostic(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	p.zvec = &lockFailZvecStore{}
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	diag, ok := payload["diagnostics"].(map[string]any)
	if !ok || diag["duplicate_stdio_suspected"] != true {
		t.Fatalf("diagnostics=%v", payload["diagnostics"])
	}
}

func TestPhase1CheckUpdateContextCanceled(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.CheckUpdate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckUpdate err=%v", err)
	}
}

func TestPhase1SemanticSearchLockRecovery(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	store := &searchLockZvecStore{}
	p.zvec = store
	raw, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	results, ok := payload["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("payload=%v attempts=%d", payload, store.attempts)
	}
	if store.attempts < 2 {
		t.Fatalf("attempts=%d, want retry after lock recovery", store.attempts)
	}
}

type docCountErrZvecStore struct{ mockZvecStore }

func (s *docCountErrZvecStore) DocCount() (int, error) {
	return 0, errors.New("doc count failed")
}

func TestPhase1GetIndexStatusContextCanceled(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.GetIndexStatus(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetIndexStatus err=%v", err)
	}
}

func TestPhase1GetIndexStatusDocCountError(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	p.zvec = &docCountErrZvecStore{}
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["zvec_error"] != "doc count failed" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestPhase1GetIndexStatusProfileError(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	settings := phase1Settings(t, srv.URL+"/v1")
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	settings.App.ActiveProfile = "missing"
	p.Settings = settings
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	msg, _ := payload["message"].(string)
	if msg == "" || !strings.Contains(msg, "missing") {
		t.Fatalf("payload=%v", payload)
	}
}

func TestPhase1GetIndexStatusOnnxModelPath(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.onnx")
	if err := os.WriteFile(modelPath, []byte("onnx"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := &config.Settings{
		WorkspaceRoot: dir,
		IndexDir:      filepath.Join(dir, "index"),
		ConfigPath:    filepath.Join(dir, "config.yaml"),
		App: config.AppConfig{
			ActiveProfile: "onnx",
			Profiles: map[string]config.EmbeddingProfile{
				"onnx": {
					Provider:   "onnx",
					ModelPath:  modelPath,
					Dimensions: 3,
				},
			},
		},
	}
	p := &Phase1{
		Settings:    settings,
		searchStats: NewSearchStats(settings.App.Search),
		zvec:        &mockZvecStore{},
		zvecCfg: zvec.Config{
			IndexDir:      settings.IndexDir,
			WorkspaceRoot: settings.WorkspaceRoot,
			ProfileName:   settings.App.ActiveProfile,
			Dimensions:    3,
		},
	}
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["embedding_model_path"] != "model.onnx" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestPhase1GetIndexStatusStartupMessage(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	p.startupMsg = "migration finished"
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["message"] != "migration finished" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestPhase1ReadyActiveProfileError(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	settings := phase1Settings(t, srv.URL+"/v1")
	settings.App.ActiveProfile = "missing"
	_, err := NewPhase1(settings)
	if err == nil {
		t.Fatal("expected profile error from NewPhase1")
	}
}

func TestPhase1ReadyOpenZvecGenericError(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	indexDir := p.Settings.IndexDir
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.EnsureIndexMeta(indexDir, zvec.IndexIdentity{
		WorkspaceID:      p.Settings.WorkspaceID,
		WorkspaceRoot:    p.Settings.WorkspaceRoot,
		Profile:          p.Settings.App.ActiveProfile,
		Dimensions:       3,
		ChunkingVersion:  p.Settings.App.Indexing.Chunking.Version,
		ChunkingStrategy: p.Settings.App.Indexing.Chunking.Strategy,
	}); err != nil {
		t.Fatal(err)
	}
	p.zvec = &mockZvecStore{openErr: errors.New("zvec open failed")}
	if err := p.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "zvec open failed") {
		t.Fatalf("Ready err=%v", err)
	}
}

func TestOpenZvecWithRecoverySkipsWhenOpen(t *testing.T) {
	store := &mockZvecStore{open: true}
	p := &Phase1{zvec: store}
	if err := p.openZvecWithRecovery(); err != nil {
		t.Fatalf("openZvecWithRecovery: %v", err)
	}
	if store.closeCalls != 0 {
		t.Fatalf("closeCalls=%d, want 0", store.closeCalls)
	}
}

type lockRecoverZvecStore struct {
	mockZvecStore
	openAttempts int
}

func (s *lockRecoverZvecStore) Open() error {
	s.openAttempts++
	if s.openAttempts == 1 {
		return errors.New(`Can't open lock file: test lock`)
	}
	s.open = true
	return nil
}

func TestOpenZvecWithRecoveryNoCloseWhileIndexing(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.Settings.IndexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prog := indexer.NewProgressStore(p.Settings.IndexDir)
	if err := prog.Save(indexer.Progress{State: indexer.StateRunning, Running: true}); err != nil {
		t.Fatal(err)
	}
	p.coordinator = nil
	store := &lockRecoverZvecStore{}
	p.zvec = store
	if err := p.openZvecWithRecovery(); err != nil {
		t.Fatalf("openZvecWithRecovery: %v", err)
	}
	if store.closeCalls != 0 {
		t.Fatalf("closeCalls=%d, want 0 while indexing", store.closeCalls)
	}
	if !store.open {
		t.Fatal("expected zvec open after recovery")
	}
}

type blockingSearchZvecStore struct {
	mockZvecStore
	searchEntered chan struct{}
	allowSearch   chan struct{}
	enterOnce     sync.Once
}

func (s *blockingSearchZvecStore) Search(vector []float32, topK int, pathGlob string) ([]zvec.SearchHit, error) {
	s.enterOnce.Do(func() { close(s.searchEntered) })
	<-s.allowSearch
	return s.mockZvecStore.Search(vector, topK, pathGlob)
}

func TestPhase1ShutdownWaitsForSearch(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	store := &blockingSearchZvecStore{
		mockZvecStore: mockZvecStore{hits: []zvec.SearchHit{{Path: "a.go", Score: 1, Snippet: "x"}}},
		searchEntered: make(chan struct{}),
		allowSearch:   make(chan struct{}),
	}
	p.zvec = store

	searchDone := make(chan struct{})
	go func() {
		_, _ = p.SemanticSearch(context.Background(), SearchRequest{Query: "hello"})
		close(searchDone)
	}()

	select {
	case <-store.searchEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("search did not reach zvec")
	}

	shutdownDone := make(chan struct{})
	go func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		t.Fatal("shutdown finished before search")
	case <-time.After(100 * time.Millisecond):
	}

	close(store.allowSearch)

	select {
	case <-searchDone:
	case <-time.After(2 * time.Second):
		t.Fatal("search did not finish")
	}
	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after search")
	}
	if store.closeCalls != 1 {
		t.Fatalf("closeCalls=%d, want 1", store.closeCalls)
	}
}

func TestSemanticSearchContextCancelDuringZvec(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	store := &blockingSearchZvecStore{
		mockZvecStore: mockZvecStore{hits: []zvec.SearchHit{{Path: "a.go", Score: 1, Snippet: "x"}}},
		searchEntered: make(chan struct{}),
		allowSearch:   make(chan struct{}),
	}
	p.zvec = store

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := p.SemanticSearch(ctx, SearchRequest{Query: "auth"})
		done <- err
	}()

	select {
	case <-store.searchEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("search did not reach zvec")
	}
	cancel()

	select {
	case err := <-done:
		t.Fatal("search returned before zvec finished:", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(store.allowSearch)

	select {
	case err := <-done:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("search did not return after zvec finished")
	}
}

func TestPhase1ShutdownWaitsAfterSearchCancelDuringZvec(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	store := &blockingSearchZvecStore{
		mockZvecStore: mockZvecStore{hits: []zvec.SearchHit{{Path: "a.go", Score: 1, Snippet: "x"}}},
		searchEntered: make(chan struct{}),
		allowSearch:   make(chan struct{}),
	}
	p.zvec = store

	ctx, cancel := context.WithCancel(context.Background())
	searchDone := make(chan struct{})
	go func() {
		_, _ = p.SemanticSearch(ctx, SearchRequest{Query: "auth"})
		close(searchDone)
	}()

	select {
	case <-store.searchEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("search did not reach zvec")
	}
	cancel()

	shutdownDone := make(chan struct{})
	go func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		t.Fatal("shutdown finished before zvec search completed")
	case <-time.After(200 * time.Millisecond):
	}

	close(store.allowSearch)

	select {
	case <-searchDone:
	case <-time.After(2 * time.Second):
		t.Fatal("search did not finish")
	}
	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after search")
	}
	if store.closeCalls != 1 {
		t.Fatalf("closeCalls=%d, want 1", store.closeCalls)
	}
}

func TestSemanticSearchNegativeLimit(t *testing.T) {
	p, err := NewPhase1(phase1Settings(t, "http://127.0.0.1:9/v1"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.SemanticSearch(context.Background(), SearchRequest{Query: "hello", Limit: -1})
	if err == nil || !strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("err=%v", err)
	}
}

func TestNormalizeSearchLimit(t *testing.T) {
	topK := 7
	got, err := normalizeSearchLimit(0, &topK)
	if err != nil || got != 7 {
		t.Fatalf("got=%d err=%v", got, err)
	}
	if _, err := normalizeSearchLimit(-5, nil); err == nil {
		t.Fatal("expected negative limit error")
	}
}

func TestSemanticSearchSkipsLockRecoveryWhileIndexing(t *testing.T) {
	srv := modelsEmbedServer(t)
	defer srv.Close()
	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitCoordinatorIdle(t, p) })
	store := &searchLockZvecStore{}
	p.zvec = store
	if _, err := p.Reindex(context.Background(), ReindexRequest{Force: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if store.attempts != 1 {
		t.Fatalf("attempts=%d, want no retry while indexing holds zvec lock", store.attempts)
	}
	msg, _ := payload["message"].(string)
	if !strings.Contains(msg, "vector search failed") {
		t.Fatalf("payload=%v", payload)
	}
}

func TestStartAutoIndexWhenAlreadyRunning(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	settings.AutoIndexOnStart = true
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitCoordinatorIdle(t, p) })
	if _, err := p.coordinator.Start(true); err != nil {
		t.Fatal(err)
	}
	p.StartAutoIndex()
	if !p.coordinator.IsRunning() {
		t.Fatal("expected indexing still running")
	}
}
