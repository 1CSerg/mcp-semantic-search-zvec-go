//go:build realworld && zvec

package scenarios

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func TestEnvPathPrecedence(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	const token = "env-precedence-token"
	harness.WithEnvFile(t, repo, map[string]string{
		"API_TOKEN": token,
	})

	srv := harness.StartHTTPServer(t, repo, 19380)
	status, code := harness.GetJSONAuth(t, srv.HTTPBase+"/v1/status", token)
	if code != 200 {
		t.Fatalf("API_TOKEN from .env via ENV_PATH: code=%d", code)
	}
	if status["workspace_root"] == "" {
		t.Fatalf("authenticated status empty: %v", status)
	}
}

func TestWorkspaceRootEnvOverride(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	corpus := harness.CorpusDir(repo)
	srv := harness.StartHTTPServerWithEnv(t, repo, 19381, harness.EnvWithOverrides(repo, map[string]string{
		"WORKSPACE_ROOT": corpus,
	}))
	t.Cleanup(func() { harness.KillProcessGraceful(t, srv.Cmd) })

	status := harness.GetJSON(t, srv.HTTPBase+"/v1/status")
	root, _ := status["workspace_root"].(string)
	if !strings.EqualFold(filepath.Clean(root), filepath.Clean(corpus)) {
		t.Fatalf("WORKSPACE_ROOT override: got %q want %q", root, corpus)
	}
}

func TestConfigPathEnvOverride(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	const marker = "realworld_config_path_marker"
	const mockPort = 19384
	harness.StartMockEmbed(t, repo, mockPort, 128, false)
	cfg := harness.WriteDetectableMockConfig(t, repo, marker, mockPort)

	srv := harness.StartHTTPServerWithEnv(t, repo, 19382, harness.EnvWithOverrides(repo, map[string]string{
		"CONFIG_PATH": cfg,
	}))
	t.Cleanup(func() { harness.KillProcessGraceful(t, srv.Cmd) })

	status := harness.GetJSON(t, srv.HTTPBase+"/v1/status")
	if got, _ := status["active_profile"].(string); got != marker {
		t.Fatalf("CONFIG_PATH override: active_profile=%q want %q status=%v", got, marker, status)
	}
}
