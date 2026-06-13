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
	hits       []zvec.SearchHit
	err        error
	openErr    error
	wipeErr    error
	wipeCalled bool
}

func (m *mockZvecStore) Open() error {
	if m.openErr != nil {
		return m.openErr
	}
	return nil
}
func (m *mockZvecStore) Close() error                                 { return nil }
func (m *mockZvecStore) DocCount() (int, error)                       { return len(m.hits), nil }
func (m *mockZvecStore) UpsertChunks([]zvec.Chunk, [][]float32) error { return nil }
func (m *mockZvecStore) DeleteByIDs([]string) error                   { return nil }
func (m *mockZvecStore) WipeCollection() error {
	m.wipeCalled = true
	return m.wipeErr
}
func (m *mockZvecStore) Search([]float32, int, string) ([]zvec.SearchHit, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.hits, nil
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
		p, err := NewPhase1(settings)
		if err != nil {
			t.Fatalf("NewPhase1: %v", err)
		}
		if p.embed != nil {
			t.Fatal("expected no embed client")
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
	raw, err := p.GetIndexStatus()
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

	raw, err := p.SemanticSearch(SearchRequest{Query: "hello"})
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

	raw, err := p.SemanticSearch(SearchRequest{Query: "auth"})
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
	if _, ok := payload["timing"]; !ok {
		t.Fatalf("missing timing: %v", payload)
	}
	if perf, ok := payload["performance"].(map[string]any); !ok || perf["degraded"] == nil {
		t.Fatalf("missing performance: %v", payload["performance"])
	}
}

func TestPhase1SemanticSearchNoEmbed(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	settings.App.ActiveProfile = ""
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.SemanticSearch(SearchRequest{Query: "hello"})
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
	raw, err := p.SemanticSearch(SearchRequest{Query: "hello"})
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
	if err := zvec.EnsureIndexMeta(p.Settings.IndexDir, p.Settings.WorkspaceID, p.Settings.WorkspaceRoot, p.Settings.App.ActiveProfile, 3); err != nil {
		t.Fatal(err)
	}
	if err := p.Ready(); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if _, err := p.Reindex(ReindexRequest{Force: true}); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	raw, err := p.CheckUpdate()
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
	_ = context.Background()
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
	raw, err := p.GetIndexStatus()
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
	p, err := NewPhase1(phase1Settings(t, "http://127.0.0.1:9/v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitCoordinatorIdle(t, p) })
	p.zvec = &mockZvecStore{}
	if _, err := p.Reindex(ReindexRequest{Force: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := p.SemanticSearch(SearchRequest{Query: "auth"})
	if err != ErrIndexingInProgress {
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
}

func TestReindexNoCoordinator(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	settings.App.ActiveProfile = ""
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.Reindex(ReindexRequest{Force: true})
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
	if _, err := p.Reindex(ReindexRequest{Force: true}); err != nil {
		t.Fatal(err)
	}
	if err := p.Ready(); err == nil {
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
	if _, err := p.SemanticSearch(SearchRequest{Query: "hello"}); err != nil {
		t.Fatal(err)
	}
	raw, err := p.GetIndexStatus()
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
	raw, err := p.GetIndexStatus()
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
	raw, err := p.GetIndexStatus()
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
	raw, err := p.SemanticSearch(SearchRequest{Query: "x"})
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
	settings.App.ActiveProfile = ""
	if err := os.MkdirAll(settings.IndexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := indexer.NewProgressStore(settings.IndexDir).Save(indexer.StartRunning(false)); err != nil {
		t.Fatal(err)
	}
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.SemanticSearch(SearchRequest{Query: "auth"})
	if err != ErrIndexingInProgress {
		t.Fatalf("err=%v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	idx, _ := payload["indexing"].(map[string]any)
	if idx["running"] != true {
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
	if _, err := p.Reindex(ReindexRequest{Force: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := p.Reindex(ReindexRequest{Force: true})
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
	settings.App.ActiveProfile = ""
	if err := os.MkdirAll(settings.IndexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := indexer.NewProgressStore(settings.IndexDir)
	if err := store.Save(indexer.StartRunning(false)); err != nil {
		t.Fatal(err)
	}
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.GetIndexStatus()
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
	if _, err := p.SemanticSearch(SearchRequest{Query: "x", PathGlob: &glob}); err != nil {
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
	if err := p.Ready(); err == nil || err.Error() != "index not built yet" {
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
	if err := zvec.EnsureIndexMeta(p.Settings.IndexDir, p.Settings.WorkspaceID, p.Settings.WorkspaceRoot, p.Settings.App.ActiveProfile, 3); err != nil {
		t.Fatal(err)
	}
	if err := p.Ready(); err == nil || !strings.Contains(err.Error(), "embeddings unreachable") {
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
	if err := zvec.EnsureIndexMeta(p.Settings.IndexDir, p.Settings.WorkspaceID, p.Settings.WorkspaceRoot, p.Settings.App.ActiveProfile, 3); err != nil {
		t.Fatal(err)
	}
	p.zvec = &mockZvecStore{openErr: zvec.ErrCollectionMissing}
	if err := p.Ready(); err == nil || err.Error() != "index not built yet" {
		t.Fatalf("Ready: %v", err)
	}
}

func TestPhase1Close(t *testing.T) {
	p := &Phase1{zvec: &mockZvecStore{}}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
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
			Indexing: config.IndexingConfig{
				Extensions:       []string{".go"},
				LockStaleSeconds: 300,
				StallSeconds:     120,
			},
		},
	}, indexDir
}

func TestPrepareStartupSkipsWhenVersionsMatch(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, true)
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{ZvecGoVersion: version.ZvecGoVersion}); err != nil {
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
	if store.wipeCalled {
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
	raw, err := p.GetIndexStatus()
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

	if !store.wipeCalled {
		t.Fatal("expected wipe during migration")
	}
	meta, err := zvec.ReadIndexMeta(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ZvecGoVersion != version.ZvecGoVersion {
		t.Fatalf("meta=%+v", meta)
	}
	waitCoordinatorIdle(t, p)
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

	if !store.wipeCalled {
		t.Fatal("expected wipe during migration")
	}
	if coord.IsRunning() {
		t.Fatal("expected no background reindex when AUTO_INDEX_ON_START=false")
	}
	if p.startupMsg == "" {
		t.Fatal("expected startup message")
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

	raw, err := p.GetIndexStatus()
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
	if store.wipeCalled {
		t.Fatal("unexpected wipe on corrupt meta")
	}
	waitCoordinatorIdle(t, p)
}
