//go:build windows

package lifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// StartTestStdioHelper starts a subprocess that looks like --stdio MCP for workspace (tests only).
func StartTestStdioHelper(t *testing.T, workspace string) *exec.Cmd {
	t.Helper()
	name := "mcp-semantic-search-zvec-go.exe"
	helper := filepath.Join(workspace, name)
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Skipf("read test binary: %v", err)
	}
	if err := os.WriteFile(helper, data, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(helper, "-test.run=TestHelperStaleStdio", "-test.v", "--stdio", workspace)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Skipf("start helper: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cmd.Process != nil {
			return cmd
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	t.Fatalf("helper not ready within %v", 5*time.Second)
	return cmd
}
