//go:build unix

package lifecycle

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

func listStdioPIDs(workspace string, selfPID int) ([]procCandidate, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read /proc: %w", err)
	}

	var pids []procCandidate
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
		// Keep the raw NUL-separated cmdline: strings.Contains matches across
		// NULs, and firstExecutableToken splits on the first NUL so paths with
		// spaces (e.g. "/opt/my app/bin") are not truncated. Replacing NUL with
		// space here would break such paths.
		if !matchesStaleStdio(string(cmdline), workspace, pid, selfPID) {
			continue
		}
		pids = append(pids, procCandidate{PID: pid, StartTime: lock.ProcessStartTime(pid)})
	}
	return pids, nil
}

func listStdioPIDsForIndexDir(indexDir string, selfPID int) ([]procCandidate, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read /proc: %w", err)
	}

	var pids []procCandidate
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
		if !matchesStdioIndexDir(string(cmdline), indexDir, pid, selfPID) {
			continue
		}
		pids = append(pids, procCandidate{PID: pid, StartTime: lock.ProcessStartTime(pid)})
	}
	return pids, nil
}

func stopStaleStdioInstances(workspace string, selfPID int) ([]int, error) {
	cands, err := listStdioPIDs(workspace, selfPID)
	if err != nil {
		return nil, err
	}
	var stopped []int
	for _, c := range cands {
		// Re-check identity by start time right before signalling, to defend
		// against pid reuse between the /proc scan and the kill.
		if err := terminatePIDChecked(c.PID, c.StartTime); err != nil {
			if !lock.ProcessAlive(c.PID) {
				stopped = append(stopped, c.PID)
				continue
			}
			slog.Warn("stale stdio scan: terminate failed", "pid", c.PID, "err", err)
			continue
		}
		stopped = append(stopped, c.PID)
	}
	return stopped, nil
}

func terminatePID(pid int, recordedStart int64) error {
	if !lock.ProcessAlive(pid) {
		return fmt.Errorf("process %d not found", pid)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = proc.Signal(syscall.SIGTERM)
	time.Sleep(killGrace)
	if !sameLiveProcess(pid, recordedStart) {
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
