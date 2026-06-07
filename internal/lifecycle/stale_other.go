//go:build !unix && !windows

package lifecycle

import "log/slog"

func stopStaleStdioInstances(workspace string, selfPID int) ([]int, error) {
	slog.Warn("stale stdio cleanup not implemented on this platform", "workspace", workspace)
	return nil, nil
}
