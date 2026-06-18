//go:build realworld && zvec

package scenarios

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func TestInstallLayoutSmoke(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	target := harness.InstallSmokeTarget(repo)
	if err := os.RemoveAll(target); err != nil {
		t.Fatalf("clean target: %v", err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(target) })

	runInstall(t, repo, target)
	assertInstallLayout(t, target)
}

func runInstall(t *testing.T, repo, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		script := filepath.Join(repo, "scripts", "install", "install.ps1")
		cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script,
			"-TargetRoot", target)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("install.ps1: %v\n%s", err, out)
		}
		return
	}
	script := filepath.Join(repo, "scripts", "install", "install.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "TARGET_ROOT="+target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
}

func assertInstallLayout(t *testing.T, target string) {
	t.Helper()
	installDir := filepath.Join(target, ".mcp-semantic-search-zvec-go")
	binName := "mcp-semantic-search-zvec-go"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	bin := filepath.Join(installDir, "bin", binName)
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("missing installed binary: %s", bin)
	}

	if runtime.GOOS == "windows" {
		launcher := filepath.Join(installDir, "bin", "run-mcp-stdio.ps1")
		if _, err := os.Stat(launcher); err != nil {
			t.Fatalf("missing launcher: %s", launcher)
		}
	}

	mcpJSON := filepath.Join(target, ".cursor", "mcp.json")
	data, err := os.ReadFile(mcpJSON)
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "semantic-search-zvec-go") {
		t.Fatalf("mcp.json missing server key: %s", text)
	}
	if runtime.GOOS == "windows" && strings.Contains(text, `"WORKSPACE_ROOT": "`) {
		if strings.Contains(text, "/") && strings.Contains(text, "WORKSPACE_ROOT") {
			// allow URL-like values only in non-path fields
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, "WORKSPACE_ROOT") && strings.Contains(line, "/") {
					t.Fatalf("forward slash in WORKSPACE_ROOT: %s", line)
				}
			}
		}
	}

	rulePath := filepath.Join(target, ".cursor", "rules", "semantic-search-zvec-go.mdc")
	rule, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("read cursor rule: %v", err)
	}
	if !strings.Contains(string(rule), "managedBy: mcp-semantic-search-zvec-go") {
		t.Fatalf("cursor rule missing managedBy marker")
	}

	envPath := filepath.Join(installDir, ".env")
	if _, err := os.Stat(envPath); err != nil {
		t.Fatalf("missing .env: %s", envPath)
	}
}
