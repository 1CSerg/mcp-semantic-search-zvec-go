//go:build unix

package lifecycle

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

func stopLauncherWrappers(workspace string, exclude map[int]int64) ([]int, error) {
	installDir := filepath.Join(workspace, config.DefaultInstallDirName)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read /proc: %w", err)
	}

	var stopped []int
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(ent.Name())
		if err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", ent.Name(), "cmdline"))
		if err != nil {
			slog.Warn("launcher scan: read cmdline failed", "pid", pid, "err", err)
			continue
		}
		// Keep the raw NUL-separated cmdline so paths containing spaces are not
		// truncated by firstExecutableToken and substring matches stay intact.
		if !matchesLauncherWrapper(string(cmdline), workspace, installDir) {
			continue
		}
		if isExcludedPID(pid, exclude) {
			continue
		}
		// Capture start time now and re-check identity before kill to defend
		// against pid reuse between the scan and the signal.
		startTime := lock.ProcessStartTime(pid)
		if err := terminatePIDChecked(pid, startTime); err != nil {
			slog.Warn("launcher scan: terminate failed", "pid", pid, "err", err)
			continue
		}
		stopped = append(stopped, pid)
	}
	return stopped, nil
}

func matchesLauncherWrapper(cmdline, workspace, installDir string) bool {
	lower := strings.ToLower(cmdline)
	if !strings.Contains(lower, "mcp-semantic-search-zvec-go") {
		return false
	}
	if pathContainsPath(cmdline, installDir) {
		return true
	}
	return pathContainsPath(cmdline, workspace)
}
