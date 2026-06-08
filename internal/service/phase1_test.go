package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

type mockZvecStore struct {
	hits    []zvec.SearchHit
	err     error
	openErr error
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
func (m *mockZvecStore) WipeCollection() error                        { return nil }
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
	if payload["phase"] != "2" {
		t.Fatalf("phase=%v", payload["phase"])
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
}

func TestPhase1SemanticSearchNoEmbed(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	settings.App.Profiles["test"] = config.EmbeddingProfile{Provider: "onnx"}
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
	p, err := NewPhase1(phase1Settings(t, "http://127.0.0.1:9/v1"))
	if err != nil {
		t.Fatal(err)
	}
	p.zvec = &mockZvecStore{}
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
	p.zvec = &mockZvecStore{}
	if _, err := p.Reindex(ReindexRequest{Force: true}); err != nil {
		t.Fatal(err)
	}
	if err := p.Ready(); err == nil {
		t.Fatal("expected indexing in progress error")
	}
}

func TestStartAutoIndex(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	settings.AutoIndexOnStart = true
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	p.StartAutoIndex()
	if p.coordinator == nil {
		t.Fatal("expected coordinator")
	}
}

func TestReadyMissingCollection(t *testing.T) {
	p, err := NewPhase1(phase1Settings(t, "http://127.0.0.1:9/v1"))
	if err != nil {
		t.Fatal(err)
	}
	p.zvec = &mockZvecStore{openErr: zvec.ErrCollectionMissing}
	if err := p.Ready(); err == nil || err.Error() != "index not built yet" {
		t.Fatalf("Ready: %v", err)
	}
}
