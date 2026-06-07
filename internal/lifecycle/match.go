package lifecycle

import (
	"path/filepath"
	"strings"
)

const binaryName = "mcp-semantic-search-zvec-go"

// matchesStaleStdio reports whether cmdline is a prior stdio MCP instance for workspace.
func matchesStaleStdio(cmdline, workspace string, pid, selfPID int) bool {
	if pid == selfPID || pid <= 0 {
		return false
	}
	if !strings.Contains(cmdline, "--stdio") {
		return false
	}
	if !strings.Contains(strings.ToLower(cmdline), binaryName) {
		return false
	}
	return cmdlineContainsWorkspace(cmdline, workspace)
}

func cmdlineContainsWorkspace(cmdline, workspace string) bool {
	workspace = filepath.Clean(workspace)
	if workspace == "" {
		return false
	}
	if strings.Contains(cmdline, workspace) {
		return true
	}
	// Windows paths may differ by slash style in cmdline vs env.
	alt := strings.ReplaceAll(workspace, `\`, `/`)
	if alt != workspace && strings.Contains(cmdline, alt) {
		return true
	}
	alt = strings.ReplaceAll(workspace, `/`, `\`)
	return alt != workspace && strings.Contains(cmdline, alt)
}
