package lifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestMatchesStaleStdioViaWorkspaceMarker(t *testing.T) {
	staging := t.TempDir()
	workspace := filepath.Join(staging, "project-root")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(staging, config.WorkspaceRootMarkerFile),
		[]byte(workspace),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	exe := filepath.Join(staging, "mcp-semantic-search-zvec-go.exe")
	cmdline := `"` + exe + `" --stdio`
	if !matchesStaleStdio(cmdline, workspace, 5678, 1234) {
		t.Fatal("expected match via workspace-root marker")
	}
}
