package service

import (
	"encoding/json"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestStubGetIndexStatus(t *testing.T) {
	s := NewStub(&config.Settings{
		WorkspaceRoot: "/workspace",
		IndexDir:      "/workspace/.mcp-semantic-search-zvec-go/data/index",
		ConfigPath:    "/workspace/.mcp-semantic-search-zvec-go/config.yaml",
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible", Model: "test-model", Dimensions: 384},
			},
		},
	})

	raw, err := s.GetIndexStatus()
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
}

func TestStubSemanticSearch(t *testing.T) {
	s := NewStub(&config.Settings{App: config.AppConfig{ActiveProfile: "x", Profiles: map[string]config.EmbeddingProfile{"x": {}}}})
	raw, err := s.SemanticSearch(SearchRequest{Query: "auth middleware"})
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
	if err := s.Ready(); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}

func TestStubReindex(t *testing.T) {
	s := NewStub(&config.Settings{})
	raw, err := s.Reindex(ReindexRequest{Force: true})
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
	raw, err := s.CheckUpdate()
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
	raw, err := s.SemanticSearch(SearchRequest{Query: "q", TopK: &topK})
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
