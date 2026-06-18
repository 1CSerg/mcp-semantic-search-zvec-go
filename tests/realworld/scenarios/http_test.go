//go:build realworld && zvec

package scenarios

import (
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func TestHTTPTransport(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := harness.StartHTTPServer(t, repo, 19301)
	harness.ForceReindex(t, srv.HTTPBase)
	status := harness.WaitIndexIdle(t, srv.HTTPBase)

	if n, ok := status["zvec_doc_count"].(float64); !ok || n < 1 {
		t.Fatalf("expected zvec_doc_count >= 1, status=%v", status)
	}

	ready := harness.GetJSON(t, srv.HTTPBase+"/ready")
	if ready["status"] != "ready" {
		t.Fatalf("/ready: %v", ready)
	}

	hit := harness.AssertSearchHit(t, srv.HTTPBase, "REALWORLD_GO_AUTH_GATE", "middleware.go", "", "ast")
	snippet, _ := hit["snippet"].(string)
	if !strings.Contains(snippet, "REALWORLD_GO_AUTH_GATE") {
		t.Fatalf("snippet missing marker: %q", snippet)
	}
}

func TestHTTPStatusEndpoints(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := harness.StartHTTPServer(t, repo, 19302)
	health := harness.GetJSON(t, srv.HTTPBase+"/health")
	if health == nil {
		t.Fatal("empty /health response")
	}
	status := harness.GetJSON(t, srv.HTTPBase+"/v1/status")
	if status["workspace_root"] == "" {
		t.Fatalf("missing workspace_root: %v", status)
	}
}
