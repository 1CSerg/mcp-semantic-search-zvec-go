package lifecycle

import "strings"

// FindStdioForWorkspace returns the PID of a live --stdio MCP process for workspace.
func FindStdioForWorkspace(workspace string, selfPID int) (int, bool) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return 0, false
	}
	pids, err := listStdioPIDs(workspace, selfPID)
	if err != nil || len(pids) == 0 {
		return 0, false
	}
	return pids[0], true
}

// FindStdioForIndexDir returns another live --stdio MCP process sharing index-dir.txt.
func FindStdioForIndexDir(indexDir string, selfPID int) (int, bool) {
	indexDir = strings.TrimSpace(indexDir)
	if indexDir == "" {
		return 0, false
	}
	pids, err := listStdioPIDsForIndexDir(indexDir, selfPID)
	if err != nil || len(pids) == 0 {
		return 0, false
	}
	return pids[0], true
}
