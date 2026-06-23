//go:build realworld && zvec

package scenarios

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func TestDuplicateStdioSuspected(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	session, cmd1 := harness.StartMCPServerSessionNoCleanup(t, repo)
	time.Sleep(2 * time.Second)
	cmd2 := harness.StartMCPServerNoCleanup(t, repo)
	t.Cleanup(func() {
		harness.KillProcessGraceful(t, cmd1)
		harness.KillProcessGraceful(t, cmd2)
	})
	time.Sleep(500 * time.Millisecond)

	status := harness.CallMCPTool(t, session, "index_status", map[string]any{})
	diag, _ := status["diagnostics"].(map[string]any)
	if diag == nil {
		t.Fatal("missing diagnostics")
	}
	dup, _ := diag["duplicate_stdio_suspected"].(bool)
	if !dup {
		t.Fatalf("expected duplicate_stdio_suspected=true, diagnostics=%v", diag)
	}
}

func TestSearchDuringIndexing(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := harness.StartHTTPServer(t, repo, 19341)
	harness.ForceReindex(t, srv.HTTPBase)

	var resp map[string]any
	observedRunning := false
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		resp = harness.PostJSON(t, srv.HTTPBase+"/v1/search", map[string]any{
			"query": "REALWORLD_GO_AUTH_GATE",
			"limit": 5,
		})
		idx, _ := resp["indexing"].(map[string]any)
		if idx != nil {
			if running, _ := idx["running"].(bool); running {
				observedRunning = true
				break
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !observedRunning {
		t.Fatalf("indexing.running never observed during poll window; last response=%v", resp)
	}
	idx, _ := resp["indexing"].(map[string]any)
	if idx == nil {
		t.Fatalf("expected indexing field during search: %v", resp)
	}
	if _, ok := resp["results"]; !ok {
		t.Fatalf("search panicked or missing results: %v", resp)
	}
}

func TestGracefulShutdownMidSearch(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := harness.StartHTTPServerNoCleanup(t, repo, 19342)
	harness.ForceReindex(t, srv.HTTPBase)
	harness.WaitIndexIdle(t, srv.HTTPBase)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = http.Post(srv.HTTPBase+"/v1/search", "application/json",
			strings.NewReader(`{"query":"REALWORLD_GO_AUTH_GATE","limit":10}`))
	}()
	time.Sleep(500 * time.Millisecond)
	harness.KillProcessGraceful(t, srv.Cmd)
	wg.Wait()
	harness.WaitPortClosed(t, 19342, 10*time.Second)
}

func TestStaleZvecLockReclaim(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := harness.StartHTTPServer(t, repo, 19343)
	harness.ForceReindex(t, srv.HTTPBase)
	harness.WaitIndexIdle(t, srv.HTTPBase)

	harness.KillProcessGraceful(t, srv.Cmd)
	harness.WriteStaleZvecLock(t, repo)
	time.Sleep(500 * time.Millisecond)

	srv2 := harness.StartHTTPServer(t, repo, 19344)
	harness.ForceReindex(t, srv2.HTTPBase)
	status := harness.WaitIndexIdle(t, srv2.HTTPBase)
	if n, ok := status["zvec_doc_count"].(float64); !ok || n < 1 {
		t.Fatalf("reindex after stale LOCK reclaim failed: %v", status)
	}
	if errMsg, _ := status["zvec_error"].(string); strings.Contains(strings.ToLower(errMsg), "lock") {
		t.Fatalf("zvec still reports lock error: %q", errMsg)
	}
}
