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
)

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
	if payload["phase"] != "1-bootstrap" {
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
