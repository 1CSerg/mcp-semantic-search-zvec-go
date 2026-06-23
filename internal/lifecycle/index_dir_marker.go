package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
)

const indexDirMarkerFile = "index-dir.txt"

func indexDirFromCmdline(cmdline string) string {
	exe := firstExecutableToken(cmdline)
	if exe == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(exe), indexDirMarkerFile))
	if err != nil {
		return ""
	}
	root := strings.TrimSpace(string(data))
	if root == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	return abs
}

func matchesStdioIndexDir(cmdline, indexDir string, pid, selfPID int) bool {
	if pid == selfPID || pid <= 0 {
		return false
	}
	if !strings.Contains(cmdline, "--stdio") {
		return false
	}
	if !strings.Contains(strings.ToLower(cmdline), binaryName) {
		return false
	}
	return pathsEqual(indexDirFromCmdline(cmdline), indexDir)
}
