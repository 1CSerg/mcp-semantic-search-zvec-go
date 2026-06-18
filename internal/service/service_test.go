package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestSearchResultItemJSONFieldOrder(t *testing.T) {
	raw, err := json.Marshal(SearchResultItem{
		StartLine:     12,
		EndLine:       45,
		Path:          "internal/auth/middleware.go",
		Score:         0.87,
		Snippet:       "...",
		SymbolName:    "Auth",
		SymbolKind:    "function",
		ParentScope:   "middleware",
		ChunkStrategy: "line_window",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"start_line":12,"end_line":45,"path":"internal/auth/middleware.go","score":0.87,"snippet":"...","symbol_name":"Auth","symbol_kind":"function","parent_scope":"middleware","chunk_strategy":"line_window"}`
	if string(raw) != want {
		t.Fatalf("got %s", raw)
	}
	if !strings.HasPrefix(string(raw), `{"start_line":`) {
		t.Fatalf("start_line must be first: %s", raw)
	}
}

func TestStubGetIndexStatusChunkingFields(t *testing.T) {
	root := t.TempDir()
	s := NewStub(&config.Settings{
		WorkspaceRoot: root,
		IndexDir:      filepath.Join(root, ".mcp-semantic-search-zvec-go", "data", "index"),
		ConfigPath:    filepath.Join(root, ".mcp-semantic-search-zvec-go", "config.yaml"),
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible", Model: "test-model", Dimensions: 384},
			},
			Indexing: config.IndexingConfig{
				Chunking: config.ChunkingConfig{
					Strategy: "hybrid",
					Version:  1,
				},
			},
		},
	})

	raw, err := s.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatalf("GetIndexStatus: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["chunking_strategy"] != "hybrid" {
		t.Fatalf("chunking_strategy=%v", payload["chunking_strategy"])
	}
	if payload["chunking_version"] != float64(1) {
		t.Fatalf("chunking_version=%v", payload["chunking_version"])
	}
}

func TestStubGetIndexStatus(t *testing.T) {
	root := t.TempDir()
	s := NewStub(&config.Settings{
		WorkspaceRoot: root,
		IndexDir:      filepath.Join(root, ".mcp-semantic-search-zvec-go", "data", "index"),
		ConfigPath:    filepath.Join(root, ".mcp-semantic-search-zvec-go", "config.yaml"),
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible", Model: "test-model", Dimensions: 384},
			},
		},
	})

	raw, err := s.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatalf("GetIndexStatus: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["bootstrap"] != true {
		t.Fatalf("expected bootstrap=true, got %v", payload["bootstrap"])
	}
	if payload["index_dir"] != ".mcp-semantic-search-zvec-go/data/index" {
		t.Fatalf("index_dir=%v", payload["index_dir"])
	}
	if payload["config_path"] != ".mcp-semantic-search-zvec-go/config.yaml" {
		t.Fatalf("config_path=%v", payload["config_path"])
	}
}

func TestStubSemanticSearch(t *testing.T) {
	s := NewStub(&config.Settings{App: config.AppConfig{ActiveProfile: "x", Profiles: map[string]config.EmbeddingProfile{"x": {}}}})
	raw, err := s.SemanticSearch(context.Background(), SearchRequest{Query: "auth middleware"})
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["query"] != "auth middleware" {
		t.Fatalf("unexpected query: %v", payload["query"])
	}
}

func TestStubReady(t *testing.T) {
	s := NewStub(&config.Settings{})
	if err := s.Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}

func TestStubReindex(t *testing.T) {
	s := NewStub(&config.Settings{})
	raw, err := s.Reindex(context.Background(), ReindexRequest{Force: true})
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["force"] != true {
		t.Fatalf("force=%v", payload["force"])
	}
}

func TestStubCheckUpdate(t *testing.T) {
	s := NewStub(&config.Settings{GitHubRepo: "org/repo"})
	raw, err := s.CheckUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["github_repo"] != "org/repo" {
		t.Fatalf("repo=%v", payload["github_repo"])
	}
}

func TestStubSemanticSearchTopK(t *testing.T) {
	topK := 5
	s := NewStub(&config.Settings{App: config.AppConfig{ActiveProfile: "x", Profiles: map[string]config.EmbeddingProfile{"x": {}}}})
	raw, err := s.SemanticSearch(context.Background(), SearchRequest{Query: "q", TopK: &topK})
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["limit"] != float64(5) {
		t.Fatalf("limit=%v", payload["limit"])
	}
}

func TestStubContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := NewStub(&config.Settings{})
	if _, err := s.SemanticSearch(ctx, SearchRequest{Query: "q"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("SemanticSearch err=%v", err)
	}
	if _, err := s.GetIndexStatus(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetIndexStatus err=%v", err)
	}
	if _, err := s.Reindex(ctx, ReindexRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reindex err=%v", err)
	}
	if _, err := s.CheckUpdate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckUpdate err=%v", err)
	}
	if err := s.Ready(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Ready err=%v", err)
	}
}
