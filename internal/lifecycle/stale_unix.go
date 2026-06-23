//go:build unix

package lifecycle

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

func listStdioPIDs(workspace string, selfPID int) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read /proc: %w", err)
	}

	var pids []int
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
			slog.Warn("stdio scan: read cmdline failed", "pid", pid, "err", err)
			continue
		}
		line := strings.ReplaceAll(string(cmdline), "\x00", " ")
		if !matchesStaleStdio(line, workspace, pid, selfPID) {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func listStdioPIDsForIndexDir(indexDir string, selfPID int) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read /proc: %w", err)
	}

	var pids []int
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
			slog.Warn("stdio scan: read cmdline failed", "pid", pid, "err", err)
			continue
		}
		line := strings.ReplaceAll(string(cmdline), "\x00", " ")
		if !matchesStdioIndexDir(line, indexDir, pid, selfPID) {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func stopStaleStdioInstances(workspace string, selfPID int) ([]int, error) {
	pids, err := listStdioPIDs(workspace, selfPID)
	if err != nil {
		return nil, err
	}
	var stopped []int
	for _, pid := range pids {
		if err := terminatePID(pid); err != nil {
			if !lock.ProcessAlive(pid) {
				stopped = append(stopped, pid)
				continue
			}
			slog.Warn("stale stdio scan: terminate failed", "pid", pid, "err", err)
			continue
		}
		stopped = append(stopped, pid)
	}
	return stopped, nil
}

func terminatePID(pid int) error {
	if !lock.ProcessAlive(pid) {
		return fmt.Errorf("process %d not found", pid)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = proc.Signal(syscall.SIGTERM)
	time.Sleep(killGrace)
	if !lock.ProcessAlive(pid) {
		return nil
	}
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil {
			if errors.Is(killErr, syscall.ESRCH) && !lock.ProcessAlive(pid) {
				return nil
			}
			return killErr
		}
	}
	return nil
}
