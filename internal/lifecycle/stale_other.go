//go:build !unix && !windows

package lifecycle

import "log/slog"

func listStdioPIDs(workspace string, selfPID int) ([]procCandidate, error) {
	slog.Warn("stdio scan not implemented on this platform", "workspace", workspace)
	return nil, ErrStdioScanUnsupported
}

func stopStaleStdioInstances(workspace string, selfPID int) ([]int, error) {
	slog.Warn("stale stdio cleanup not implemented on this platform", "workspace", workspace)
	return nil, ErrStdioScanUnsupported
}

func listStdioPIDsForIndexDir(indexDir string, selfPID int) ([]procCandidate, error) {
	return nil, ErrStdioScanUnsupported
}
