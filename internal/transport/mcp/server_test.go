package mcptransport

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
)

func TestTextResult(t *testing.T) {
	res := textResult(`{"ok":true}`)
	if len(res.Content) != 1 {
		t.Fatalf("content len=%d", len(res.Content))
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok || text.Text != `{"ok":true}` {
		t.Fatalf("content=%v", res.Content[0])
	}
}

func TestToolError(t *testing.T) {
	res, extra, err := toolError(errors.New("boom"))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if extra != nil {
		t.Fatal("expected nil extra")
	}
	if !res.IsError {
		t.Fatal("expected IsError")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "error: boom" {
		t.Fatalf("text=%v", res.Content[0])
	}
}

func TestRegisterTools(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	settings := &config.Settings{
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible"},
			},
		},
	}
	registerTools(server, service.NewStub(settings))
}

func TestMCPToolHandlers(t *testing.T) {
	settings := &config.Settings{
		WorkspaceRoot: t.TempDir(),
		GitHubRepo:    "org/repo",
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible", Model: "m", Dimensions: 384},
			},
		},
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	registerTools(server, service.NewStub(settings))

	ct, st := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	ctx := context.Background()
	cases := []struct {
		name string
		args map[string]any
	}{
		{"semantic_search", map[string]any{"query": "auth middleware"}},
		{"index_status", map[string]any{}},
		{"reindex", map[string]any{"force": true}},
		{"check_update", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{
				Name:      tc.name,
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if len(res.Content) == 0 {
				t.Fatal("empty content")
			}
			text, ok := res.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatalf("content=%T", res.Content[0])
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
				t.Fatalf("unmarshal: %v text=%s", err, text.Text)
			}
		})
	}
}

type failingMCPService struct {
	service.Service
}

func (failingMCPService) SemanticSearch(service.SearchRequest) (json.RawMessage, error) {
	return nil, errors.New("search failed")
}

func TestMCPToolHandlerError(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	registerTools(server, failingMCPService{Service: service.NewStub(&config.Settings{
		App: config.AppConfig{ActiveProfile: "x", Profiles: map[string]config.EmbeddingProfile{"x": {}}},
	})})

	ct, st := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "semantic_search",
		Arguments: map[string]any{"query": "x"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected tool error result")
	}
}
