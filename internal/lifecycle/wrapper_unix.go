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
)

func stopLauncherWrappers(workspace string) ([]int, error) {
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
		line := strings.ReplaceAll(string(cmdline), "\x00", " ")
		if !matchesLauncherWrapper(line, workspace, installDir) {
			continue
		}
		if err := terminatePID(pid); err != nil {
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
