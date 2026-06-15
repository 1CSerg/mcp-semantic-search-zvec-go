//go:build unix

package lifecycle

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func stopStaleStdioInstances(workspace string, selfPID int) ([]int, error) {
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
			slog.Warn("stale stdio scan: read cmdline failed", "pid", pid, "err", err)
			continue
		}
		line := strings.ReplaceAll(string(cmdline), "\x00", " ")
		if !matchesStaleStdio(line, workspace, pid, selfPID) {
			continue
		}
		if err := terminatePID(pid); err != nil {
			slog.Warn("stale stdio scan: terminate failed", "pid", pid, "err", err)
			continue
		}
		stopped = append(stopped, pid)
	}
	return stopped, nil
}

func terminatePID(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = proc.Signal(syscall.SIGTERM)
	time.Sleep(killGrace)
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}
