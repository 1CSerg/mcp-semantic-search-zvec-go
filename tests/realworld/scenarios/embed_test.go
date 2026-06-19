//go:build realworld && zvec

package scenarios

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func embedIndexDir(t *testing.T, repo, name string) string {
	t.Helper()
	dir := filepath.Join(harness.RealworldRoot(repo), "tmp-index", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir index: %v", err)
	}
	return dir
}

func TestEmbedServerDown(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	cfg := harness.WriteTempConfig(t, repo, "mock-fail", 0)
	idx := embedIndexDir(t, repo, "embed-down")
	srv := harness.StartHTTPServerWithConfigIndex(t, repo, cfg, idx, 19320)
	harness.ForceReindex(t, srv.HTTPBase)

	deadline := time.Now().Add(2 * time.Minute)
	var last map[string]any
	for time.Now().Before(deadline) {
		last = harness.GetJSON(t, srv.HTTPBase+"/v1/status")
		idx, _ := last["indexing"].(map[string]any)
		if idx != nil && idx["state"] == "error" {
			break
		}
		time.Sleep(800 * time.Millisecond)
	}
	raw, _ := json.Marshal(last)
	msg := strings.ToLower(string(raw))
	if !strings.Contains(msg, "error") && !strings.Contains(msg, "connect") &&
		!strings.Contains(msg, "embed") && !strings.Contains(msg, "refused") {
		t.Fatalf("expected embedding failure in status, got: %s", raw)
	}
}

func TestEmbedDimensionMismatch(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	const mockPort = 19997
	harness.StartMockEmbed(t, repo, mockPort, 128, false)
	cfg := harness.WriteTempConfig(t, repo, "mock-dim-mismatch", mockPort)
	idx := embedIndexDir(t, repo, "embed-dim-mismatch")
	srv := harness.StartHTTPServerWithConfigIndex(t, repo, cfg, idx, 19321)
	harness.ForceReindex(t, srv.HTTPBase)

	deadline := time.Now().Add(2 * time.Minute)
	var last map[string]any
	for time.Now().Before(deadline) {
		last = harness.GetJSON(t, srv.HTTPBase+"/v1/status")
		idx, _ := last["indexing"].(map[string]any)
		if idx != nil && idx["state"] == "error" {
			break
		}
		time.Sleep(800 * time.Millisecond)
	}
	raw, _ := json.Marshal(last)
	msg := strings.ToLower(string(raw))
	if !strings.Contains(msg, "dimension") && !strings.Contains(msg, "mismatch") {
		t.Fatalf("expected dimension mismatch error, status=%s", raw)
	}
}

func TestEmbedTransientRetrySuccess(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	const mockPort = 19996
	harness.StartMockEmbedFailCount(t, repo, mockPort, 128, false, 2)
	cfg := harness.WriteTempConfig(t, repo, "mock-retry", mockPort)
	idx := embedIndexDir(t, repo, "embed-retry")
	srv := harness.StartHTTPServerWithConfigIndex(t, repo, cfg, idx, 19322)
	harness.ForceReindex(t, srv.HTTPBase)
	status := harness.WaitIndexIdle(t, srv.HTTPBase)
	if n, ok := status["zvec_doc_count"].(float64); !ok || n < 1 {
		t.Fatalf("retry reindex should succeed, status=%v", status)
	}
}

func TestEmbedMissingAPIKeyEnv(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	const mockPort = 19995
	harness.StartMockEmbed(t, repo, mockPort, 128, false)
	cfg := harness.WriteTempConfig(t, repo, "mock-api-key", mockPort)
	idx := embedIndexDir(t, repo, "embed-api-key")
	// Ensure REALWORLD_TEST_API_KEY is unset in .env
	harness.WithEnvFile(t, repo, map[string]string{})
	srv := harness.StartHTTPServerWithConfigIndex(t, repo, cfg, idx, 19323)
	harness.ForceReindex(t, srv.HTTPBase)

	deadline := time.Now().Add(2 * time.Minute)
	var last map[string]any
	for time.Now().Before(deadline) {
		last = harness.GetJSON(t, srv.HTTPBase+"/v1/status")
		idx, _ := last["indexing"].(map[string]any)
		if idx != nil && idx["state"] == "error" {
			break
		}
		time.Sleep(800 * time.Millisecond)
	}
	raw, _ := json.Marshal(last)
	msg := strings.ToLower(string(raw))
	if !strings.Contains(msg, "api") && !strings.Contains(msg, "key") && !strings.Contains(msg, "REALWORLD_TEST_API_KEY") {
		t.Fatalf("expected missing api_key_env error, status=%s", raw)
	}
}
