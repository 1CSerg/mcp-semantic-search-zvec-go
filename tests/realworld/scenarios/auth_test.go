//go:build realworld && zvec

package scenarios

import (
	"net/http"
	"os"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

const testAPIToken = "realworld-test-token"

func TestDaemonBearerShowsFullPaths(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	setup, _ := harness.StartDaemonWithMock(t, repo, testAPIToken)
	ws := setup.Workspaces[0]
	harness.ReindexDaemonWorkspace(t, setup, ws.ID, testAPIToken)
	harness.WaitDaemonIndexIdle(t, setup, ws.ID, testAPIToken)

	status, code := harness.GetJSONAuth(t, setup.HTTPBase+"/v1/status?workspace_id="+ws.ID, testAPIToken)
	if code != http.StatusOK {
		t.Fatalf("status with bearer: code=%d %v", code, status)
	}
	root, _ := status["workspace_root"].(string)
	if root == "" {
		t.Fatalf("authenticated daemon should expose workspace_root: %v", status)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("workspace_root not a real path: %q err=%v", root, err)
	}
}

func TestPerProjectHTTPBearerAuth(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	harness.WithEnvFile(t, repo, map[string]string{"API_TOKEN": testAPIToken})
	srv := harness.StartHTTPServer(t, repo, 19330)
	base := srv.HTTPBase

	_, code := harness.GetJSONAuth(t, base+"/v1/status", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("no bearer: want 401 got %d", code)
	}
	_, code = harness.GetJSONAuth(t, base+"/v1/status", "wrong-token")
	if code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer: want 401 got %d", code)
	}

	status, code := harness.GetJSONAuth(t, base+"/v1/status", testAPIToken)
	if code != http.StatusOK {
		t.Fatalf("valid bearer: want 200 got %d", code)
	}
	if status["workspace_root"] == "" {
		t.Fatalf("authenticated status missing workspace_root: %v", status)
	}

	_, code = harness.PostJSONAuth(t, base+"/v1/reindex", testAPIToken, map[string]any{"force": true})
	if code != http.StatusOK {
		t.Fatalf("authenticated reindex: want 200 got %d", code)
	}
}
