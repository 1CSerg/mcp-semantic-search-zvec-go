//go:build realworld && zvec

package scenarios

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func TestCLIVersion(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	for _, flag := range []string{"--version", "-version"} {
		stdout, _, code := harness.RunCLI(t, repo, harness.BaseEnv(repo), flag)
		if code != 0 {
			t.Fatalf("%s exit=%d stdout=%q", flag, code, stdout)
		}
		if !strings.Contains(stdout, version.Version) {
			t.Fatalf("%s output missing version %q: %q", flag, version.Version, stdout)
		}
	}
}

func TestCLIConfigOverride(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	const marker = "realworld_cli_config_marker"
	const mockPort = 19374
	harness.StartMockEmbed(t, repo, mockPort, 128, false)
	cfg := harness.WriteDetectableMockConfig(t, repo, marker, mockPort)

	const port = 19375
	srv := harness.StartHTTPServerWithArgs(t, repo, port, []string{"--config", cfg})
	status := harness.GetJSON(t, srv.HTTPBase+"/v1/status")
	if got, _ := status["active_profile"].(string); got != marker {
		t.Fatalf("--config override: active_profile=%q want %q status=%v", got, marker, status)
	}
}

func TestCLIHTTPAddrOverride(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	const port = 19370
	const configDefaultPort = 8080
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	srv := harness.StartHTTPServerWithArgs(t, repo, port, []string{"--http", "--http-addr", addr})
	harness.GetJSON(t, srv.HTTPBase+"/health")
	if !harness.IsPortListening("127.0.0.1", port) {
		t.Fatalf("server not listening on --http-addr port %d", port)
	}
	if harness.IsPortListening("127.0.0.1", configDefaultPort) {
		t.Fatalf("--http-addr should override config server.http_addr; port %d still listening", configDefaultPort)
	}
}

func TestCLIStopStdioForWorkspace(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	cmd := harness.StartMCPServerNoCleanup(t, repo)
	time.Sleep(2 * time.Second)
	workspace := harness.CorpusDir(repo)
	args := []string{
		"--stop-stdio-for-workspace", workspace,
		"--index-dir", harness.IndexDir(repo),
	}
	_, stderr, code := harness.RunCLI(t, repo, harness.BaseEnv(repo), args...)
	if code != 0 {
		t.Fatalf("--stop-stdio-for-workspace exit=%d stderr=%q", code, stderr)
	}
	harness.KillProcessGraceful(t, cmd)
	time.Sleep(1 * time.Second)
}

func TestCheckUpdateMCPTool(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	session := harness.StartMCPServer(t, repo)
	result := harness.CallMCPTool(t, session, "check_update", map[string]any{})
	if result["installed_version"] == "" && result["message"] == "" {
		t.Fatalf("check_update empty: %v", result)
	}
}

func TestConfigSkipDirsGit(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := harness.StartHTTPServer(t, repo, 19371)
	harness.ForceReindex(t, srv.HTTPBase)
	status := harness.WaitIndexIdle(t, srv.HTTPBase)
	raw := status
	b, _ := os.ReadFile(harness.ConfigPath(repo))
	if strings.Contains(string(b), ".git") {
		resp := harness.PostJSON(t, srv.HTTPBase+"/v1/search", map[string]any{
			"query": ".git",
			"limit": 20,
		})
		results, _ := resp["results"].([]any)
		for _, r := range results {
			item, _ := r.(map[string]any)
			path, _ := item["path"].(string)
			if strings.Contains(path, ".git"+string(os.PathSeparator)) || strings.Contains(path, "/.git/") {
				t.Fatalf(".git path indexed despite skip_dirs: %q", path)
			}
		}
	}
	_ = raw
}

func TestHTTPHealthVsReadyDuringIndex(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := harness.StartHTTPServer(t, repo, 19372)
	health, code := harness.GetJSONAuth(t, srv.HTTPBase+"/health", "")
	if code != http.StatusOK {
		t.Fatalf("/health during idle: %d", code)
	}
	_ = health

	harness.ForceReindex(t, srv.HTTPBase)
	health, _ = harness.GetJSONAuth(t, srv.HTTPBase+"/health", "")
	if health == nil {
		t.Fatal("health nil during indexing")
	}
	ready := harness.GetJSON(t, srv.HTTPBase+"/ready")
	if ready["status"] == "" {
		t.Fatalf("ready missing status during indexing: %v", ready)
	}
	harness.WaitIndexIdle(t, srv.HTTPBase)
}
