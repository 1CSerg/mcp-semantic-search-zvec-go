//go:build realworld && zvec

package scenarios

import (
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func TestDaemonMultiWorkspaceLRU(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	setup, _ := harness.StartDaemonWithMock(t, repo, "")
	for _, ws := range setup.Workspaces {
		harness.ReindexDaemonWorkspace(t, setup, ws.ID, "")
		harness.WaitDaemonIndexIdle(t, setup, ws.ID, "")
	}

	workspaces := harness.GetJSON(t, setup.HTTPBase+"/v1/workspaces")
	list, _ := workspaces["workspaces"].([]any)
	if len(list) < 3 {
		t.Fatalf("expected 3 workspaces registered, got %v", workspaces)
	}

	alpha, beta, gamma := setup.Workspaces[0], setup.Workspaces[1], setup.Workspaces[2]

	// Fill LRU max_open=2 with A and B.
	harness.TouchDaemonWorkspace(t, setup, alpha.ID, "")
	harness.TouchDaemonWorkspace(t, setup, beta.ID, "")
	open := harness.DaemonOpenWorkspaces(t, setup)
	if len(openIDs(open)) != 2 {
		t.Fatalf("LRU fill: want 2 open, got %v", open)
	}
	if !open[alpha.ID] || !open[beta.ID] {
		t.Fatalf("LRU fill: want alpha+beta open, got %v", open)
	}

	// Opening C evicts oldest idle (alpha).
	harness.TouchDaemonWorkspace(t, setup, gamma.ID, "")
	open = harness.DaemonOpenWorkspaces(t, setup)
	if len(openIDs(open)) != 2 {
		t.Fatalf("after opening gamma: want 2 open, got %v", open)
	}
	if open[alpha.ID] {
		t.Fatalf("alpha should be evicted after opening gamma, got %v", open)
	}
	if !open[beta.ID] || !open[gamma.ID] {
		t.Fatalf("beta+gamma should be open, got %v", open)
	}

	// Cold reopen alpha after eviction.
	harness.TouchDaemonWorkspace(t, setup, alpha.ID, "")
	open = harness.DaemonOpenWorkspaces(t, setup)
	if !open[alpha.ID] {
		t.Fatalf("alpha cold reopen failed, got %v", open)
	}
	if len(openIDs(open)) != 2 {
		t.Fatalf("after cold reopen alpha: want 2 open, got %v", open)
	}

	// Open daemon: reachability via zvec_doc_count + search (no workspace_root).
	for _, ws := range setup.Workspaces {
		assertDaemonWorkspaceReachable(t, setup, ws, "")
	}
}

func openIDs(open map[string]bool) []string {
	var ids []string
	for id, o := range open {
		if o {
			ids = append(ids, id)
		}
	}
	return ids
}

func assertDaemonWorkspaceReachable(t *testing.T, setup *harness.DaemonSetup, ws harness.DaemonWorkspace, bearer string) {
	t.Helper()
	status := harness.TouchDaemonWorkspace(t, setup, ws.ID, bearer)
	if _, ok := status["workspace_root"]; ok {
		if root, _ := status["workspace_root"].(string); root != "" {
			t.Fatalf("open daemon status for %s should not expose workspace_root: %v", ws.ID, status)
		}
	}
	if n, ok := status["zvec_doc_count"].(float64); !ok || n < 1 {
		t.Fatalf("status for %s missing zvec_doc_count: %v", ws.ID, status)
	}
	body := map[string]any{"query": ws.Keyword, "limit": 5, "workspace_id": ws.ID}
	headers := map[string]string{"X-Workspace-ID": ws.ID}
	resp, code := harness.PostJSONWithHeaders(t, setup.HTTPBase+"/v1/search", headers, body)
	if code >= 400 {
		t.Fatalf("search %s: status=%d %v", ws.ID, code, resp)
	}
	results, _ := resp["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("empty search for %s keyword %q", ws.ID, ws.Keyword)
	}
}

func TestDaemonOpenRedactsPaths(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	setup, _ := harness.StartDaemonWithMock(t, repo, "")
	ws := setup.Workspaces[0]
	harness.ReindexDaemonWorkspace(t, setup, ws.ID, "")
	harness.WaitDaemonIndexIdle(t, setup, ws.ID, "")

	status := harness.WaitDaemonIndexIdle(t, setup, ws.ID, "")
	for _, key := range []string{"workspace_root", "index_dir", "config_path"} {
		if v, ok := status[key]; ok && v != "" {
			t.Fatalf("open daemon should redact %q, got %v", key, v)
		}
	}

	headers := map[string]string{"X-Workspace-ID": ws.ID}
	resp, _ := harness.PostJSONWithHeaders(t, setup.HTTPBase+"/v1/search", headers, map[string]any{
		"query": ws.Keyword, "limit": 3, "workspace_id": ws.ID,
	})
	results, _ := resp["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("search returned no results: %v", resp)
	}
	harness.AssertJSONExcludesSubstring(t, "search", resp, ws.Root)
	for _, variant := range []string{ws.IndexDir, ws.ConfigPath} {
		if variant != "" {
			harness.AssertJSONExcludesSubstring(t, "search", resp, variant)
		}
	}
	// Legacy message-only check for sanitized indexing hints.
	for _, key := range []string{"message"} {
		if msg, _ := resp[key].(string); msg != "" && strings.Contains(msg, ws.Root) {
			t.Fatalf("search %q not redacted: %q", key, msg)
		}
	}
}

func TestMCPStdioProxyToDaemon(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	setup, _ := harness.StartDaemonWithMock(t, repo, "")
	ws := setup.Workspaces[0]
	harness.ReindexDaemonWorkspace(t, setup, ws.ID, "")
	harness.WaitDaemonIndexIdle(t, setup, ws.ID, "")

	session := harness.StartMCPProxy(t, repo, ws.ID, setup.HTTPBase, "")
	status := harness.CallMCPTool(t, session, "index_status", map[string]any{})
	if status["workspace_root"] == "" {
		t.Fatalf("proxy index_status missing workspace_root: %v", status)
	}

	search := harness.CallMCPTool(t, session, "semantic_search", map[string]any{
		"query": ws.Keyword,
		"limit": 5,
	})
	results, _ := search["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("proxy search empty: %v", search)
	}
}
