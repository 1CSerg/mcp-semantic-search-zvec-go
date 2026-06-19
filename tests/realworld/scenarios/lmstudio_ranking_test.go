//go:build realworld && zvec

package scenarios

import (
	"os"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func TestLMStudioSemanticRanking(t *testing.T) {
	if os.Getenv("REALWORLD_PROFILE") != "lmstudio" {
		t.Skip("requires lmstudio profile (make test-realworld-lmstudio)")
	}

	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := harness.StartHTTPServer(t, repo, 19390)
	harness.ForceReindex(t, srv.HTTPBase)
	harness.WaitIndexIdle(t, srv.HTTPBase)

	resp := harness.PostJSON(t, srv.HTTPBase+"/v1/search", map[string]any{
		"query": "authentication middleware gate",
		"limit": 10,
	})
	results, _ := resp["results"].([]any)
	if len(results) == 0 {
		t.Fatal("empty LM Studio search results")
	}
	top, _ := results[0].(map[string]any)
	topPath, _ := top["path"].(string)
	if !strings.Contains(topPath, "middleware.go") {
		t.Fatalf("LM Studio ranking: expected middleware.go first, got %q", topPath)
	}
}
