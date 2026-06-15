package lifecycle

import (
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
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
	if cmdlineContainsWorkspace(cmdline, workspace) {
		return true
	}
	return pathsEqual(workspaceFromCmdline(cmdline), workspace)
}

func workspaceFromCmdline(cmdline string) string {
	exe := firstExecutableToken(cmdline)
	if exe == "" {
		return ""
	}
	root, err := config.ReadWorkspaceRootMarker(filepath.Dir(exe))
	if err != nil {
		return ""
	}
	return root
}

func firstExecutableToken(cmdline string) string {
	cmdline = strings.TrimSpace(cmdline)
	if cmdline == "" {
		return ""
	}
	if cmdline[0] == '"' {
		end := strings.Index(cmdline[1:], `"`)
		if end >= 0 {
			return cmdline[1 : 1+end]
		}
	}
	if i := strings.Index(cmdline, " "); i >= 0 {
		return cmdline[:i]
	}
	return cmdline
}

func pathsEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if strings.EqualFold(a, b) {
		return true
	}
	altA := strings.ReplaceAll(a, `\`, `/`)
	altB := strings.ReplaceAll(b, `\`, `/`)
	return strings.EqualFold(altA, altB)
}

func normalizePathForCompare(p string) string {
	p = filepath.Clean(p)
	return strings.ToLower(strings.ReplaceAll(p, `/`, `\`))
}

func pathContainsPath(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	h := normalizePathForCompare(haystack)
	n := normalizePathForCompare(needle)
	return strings.Contains(h, n)
}

func cmdlineContainsWorkspace(cmdline, workspace string) bool {
	workspace = filepath.Clean(workspace)
	if workspace == "" {
		return false
	}
	if pathContainsPath(cmdline, workspace) {
		return true
	}
	installDir := filepath.Join(workspace, config.DefaultInstallDirName)
	return pathContainsPath(cmdline, installDir)
}
