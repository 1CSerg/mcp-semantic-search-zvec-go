package mcptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
)

func TestMCPOverHTTPProxy(t *testing.T) {
	const workspaceID = "ws-proxy"
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/status":
			if r.Header.Get("X-Workspace-ID") != workspaceID {
				t.Fatalf("status header=%q", r.Header.Get("X-Workspace-ID"))
			}
			_, _ = w.Write([]byte(`{"workspace_root":"/tmp/ws","indexing":{"running":false}}`))
		case "/v1/search":
			_, _ = w.Write([]byte(`{"query":"auth","results":[{"path":"main.go","score":0.9}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer httpSrv.Close()

	proxy := service.NewHTTPProxy(httpSrv.URL, workspaceID, "")
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	registerTools(server, proxy)

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

	statusRes, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "index_status",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("index_status: %v", err)
	}
	text, ok := statusRes.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content=%T", statusRes.Content[0])
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(text.Text), &status); err != nil {
		t.Fatal(err)
	}
	if status["workspace_root"] != "/tmp/ws" {
		t.Fatalf("status=%v", status)
	}

	searchRes, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "semantic_search",
		Arguments: map[string]any{"query": "auth"},
	})
	if err != nil {
		t.Fatalf("semantic_search: %v", err)
	}
	text, ok = searchRes.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected text content")
	}
	var search map[string]any
	if err := json.Unmarshal([]byte(text.Text), &search); err != nil {
		t.Fatal(err)
	}
	results, ok := search["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results=%v", search["results"])
	}
}
