//go:build realworld && zvec

package scenarios

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func ensureIndexedForSearch(t *testing.T, repo string, port int) *harness.ServerProcess {
	t.Helper()
	srv := harness.StartHTTPServer(t, repo, port)
	harness.ForceReindex(t, srv.HTTPBase)
	harness.WaitIndexIdle(t, srv.HTTPBase)
	return srv
}

func TestSearchPathGlob(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := ensureIndexedForSearch(t, repo, 19360)
	resp := harness.PostJSON(t, srv.HTTPBase+"/v1/search", map[string]any{
		"query":     "REALWORLD",
		"limit":     20,
		"path_glob": "**/*.go",
	})
	results, _ := resp["results"].([]any)
	if len(results) == 0 {
		t.Fatal("path_glob returned no results")
	}
	for _, r := range results {
		item, _ := r.(map[string]any)
		path, _ := item["path"].(string)
		if !strings.HasSuffix(path, ".go") {
			t.Fatalf("non-go path in glob-filtered results: %q", path)
		}
	}
}

func TestSemanticRankingAuthMiddleware(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := ensureIndexedForSearch(t, repo, 19361)
	resp := harness.PostJSON(t, srv.HTTPBase+"/v1/search", map[string]any{
		"query": "authentication middleware gate",
		"limit": 10,
	})
	results, _ := resp["results"].([]any)
	if len(results) < 2 {
		t.Skip("need at least 2 results for ranking check")
	}
	top, _ := results[0].(map[string]any)
	topPath, _ := top["path"].(string)
	if !strings.Contains(topPath, "middleware.go") {
		t.Fatalf("expected middleware.go ranked first, got %q; results=%v", topPath, results)
	}
}

func TestSearchEdgeBadQueries(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := ensureIndexedForSearch(t, repo, 19362)
	base := srv.HTTPBase

	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"empty query", map[string]any{"query": "", "limit": 5}},
		{"large limit", map[string]any{"query": "REALWORLD", "limit": 100}},
		{"glob no match", map[string]any{"query": "REALWORLD", "limit": 5, "path_glob": "**/*.nonexistent"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := harness.PostJSON(t, base+"/v1/search", tc.body)
			if _, ok := resp["results"]; !ok {
				t.Fatalf("missing results key: %v", resp)
			}
		})
	}

	// Bad JSON should not crash server.
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/search", strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bad json request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("bad json should return error status, got %d", resp.StatusCode)
	}
}

func TestSearchBeforeReadyAndDuringIndexing(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := harness.StartHTTPServer(t, repo, 19363)
	ready, code := harness.GetJSONAuth(t, srv.HTTPBase+"/ready", "")
	if code != http.StatusOK && code != http.StatusServiceUnavailable {
		t.Fatalf("GET /ready before reindex: code=%d body=%v", code, ready)
	}
	if ready["status"] != "not_ready" && ready["status"] != "ready" {
		t.Logf("/ready before reindex: %v", ready)
	}

	harness.ForceReindex(t, srv.HTTPBase)
	var during map[string]any
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		during = harness.PostJSON(t, srv.HTTPBase+"/v1/search", map[string]any{
			"query": "REALWORLD_GO_AUTH_GATE",
			"limit": 3,
		})
		idx, _ := during["indexing"].(map[string]any)
		if idx != nil {
			if running, _ := idx["running"].(bool); running {
				break
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	harness.WaitIndexIdle(t, srv.HTTPBase)
}

func TestSearchAfterFailedIndexing(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	cfg := harness.WriteTempConfig(t, repo, "mock-fail", 0)
	idx := embedIndexDir(t, repo, "search-after-fail")
	srv := harness.StartHTTPServerWithConfigIndex(t, repo, cfg, idx, 19364)
	harness.ForceReindex(t, srv.HTTPBase)

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		status := harness.GetJSON(t, srv.HTTPBase+"/v1/status")
		indexing, _ := status["indexing"].(map[string]any)
		if indexing != nil && indexing["state"] == "error" {
			break
		}
		time.Sleep(800 * time.Millisecond)
	}

	resp := harness.PostJSON(t, srv.HTTPBase+"/v1/search", map[string]any{
		"query": "anything",
		"limit": 3,
	})
	if _, ok := resp["results"]; !ok {
		t.Fatalf("search after failed indexing should not panic: %v", resp)
	}
}
