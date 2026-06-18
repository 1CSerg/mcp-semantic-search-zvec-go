//go:build realworld && zvec

package scenarios

import (
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func TestMCPStdioTransport(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	session := harness.StartMCPServer(t, repo)

	status := harness.CallMCPTool(t, session, "index_status", map[string]any{})
	if status["workspace_root"] == "" {
		t.Fatalf("index_status missing workspace_root: %v", status)
	}

	reindex := harness.CallMCPTool(t, session, "reindex", map[string]any{"force": true})
	if started, _ := reindex["started"].(bool); !started {
		t.Fatalf("reindex not started: %v", reindex)
	}

	deadline := harness.WaitIndexIdleViaMCP(t, session)
	if n, ok := deadline["zvec_doc_count"].(float64); !ok || n < 1 {
		t.Fatalf("expected indexed docs: %v", deadline)
	}

	search := harness.CallMCPTool(t, session, "semantic_search", map[string]any{
		"query": "REALWORLD_PY_HANDLER",
		"limit": 10,
	})
	results, _ := search["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("semantic_search empty: %v", search)
	}
	found := false
	for _, r := range results {
		item, _ := r.(map[string]any)
		path, _ := item["path"].(string)
		if strings.Contains(path, "handlers.py") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected handlers.py in results: %v", search)
	}
}
