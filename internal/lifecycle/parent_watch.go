package lifecycle

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

const maxAncestorDepth = 32

const disableParentWatchEnv = "MCP_DISABLE_PARENT_WATCH"

var (
	parentWatchInterval     = 2 * time.Second
	parentWatchCurrentPID   = os.Getpid
	parentWatchParentPID    = defaultParentPID
	parentWatchProcessName  = defaultProcessName
	parentWatchProcessAlive = lock.ProcessAlive
	parentWatchProcessStart = lock.ProcessStartTime
)

type watchedProcess struct {
	PID       int
	Name      string
	StartTime int64 // unix start time captured when the watch began; guards against pid reuse
}

// StartParentWatch stops the process when the stdio launch-chain ancestor exits.
func StartParentWatch(ctx context.Context, stop context.CancelFunc) {
	if parentWatchDisabled() {
		slog.Info("stdio parent watch disabled", "env", disableParentWatchEnv)
		return
	}

	chain := launchChainProcesses()
	if len(chain) == 0 {
		slog.Warn("stdio parent watch skipped: no parent process found")
		return
	}

	slog.Info("stdio parent watch started", "processes", formatWatchedProcesses(chain))
	for _, proc := range chain {
		if !parentWatchProcessAlive(proc.PID) {
			slog.Warn("stdio parent already exited", "pid", proc.PID, "name", proc.Name)
			stop()
			return
		}
	}

	go watchParents(ctx, stop, chain)
}

func watchParents(ctx context.Context, stop context.CancelFunc, chain []watchedProcess) {
	ticker := time.NewTicker(parentWatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, proc := range chain {
				if parentWatchProcessAlive(proc.PID) && sameProcess(proc.PID, proc.StartTime) {
					continue
				}
				// Either the pid is gone, or the pid was recycled: the original
				// ancestor (identified by pid+start time) no longer exists, so the
				// launch chain is broken and the stdio server should shut down.
				slog.Warn("stdio parent exited, shutting down", "pid", proc.PID, "name", proc.Name)
				stop()
				return
			}
		}
	}
}

// sameProcess reports whether pid still refers to the process that started at
// recordedStart. A recordedStart <= 0 means start time is unavailable on this
// platform (e.g. non-Linux/BSD unix); in that case we cannot verify identity
// and rely on ProcessAlive alone, preserving the historical behaviour.
func sameProcess(pid int, recordedStart int64) bool {
	if recordedStart <= 0 {
		return true
	}
	currentStart := parentWatchProcessStart(pid)
	if currentStart <= 0 {
		return true
	}
	diff := currentStart - recordedStart
	if diff < 0 {
		diff = -diff
	}
	return diff <= 1
}

// launchChainPIDSet returns self and ancestor PIDs for the current process,
// keyed by pid to the process start time. The start time is used together with
// the pid to avoid killing the active stdio launcher wrapper during PrepareStdio
// AND to defend against pid reuse: a recycled pid will report a different start
// time than the original ancestor we recorded, so it won't be wrongly excluded.
func launchChainPIDSet() map[int]int64 {
	set := make(map[int]int64)
	pid := parentWatchCurrentPID()
	for depth := 0; depth < maxAncestorDepth && pid > 0; depth++ {
		set[pid] = parentWatchProcessStart(pid)
		parent := parentWatchParentPID(pid)
		if parent <= 0 || parent == pid {
			break
		}
		pid = parent
	}
	return set
}

// isExcludedPID reports whether pid matches an entry in exclude by identity,
// i.e. same pid AND (when the recorded start time is available) the same start
// time within a 1s tolerance. If the recorded start time is 0 (unknown), it
// falls back to a pure pid match — the historical behavior.
func isExcludedPID(pid int, exclude map[int]int64) bool {
	if pid <= 0 || exclude == nil {
		return false
	}
	recordedStart, ok := exclude[pid]
	if !ok {
		return false
	}
	if recordedStart <= 0 {
		return true
	}
	currentStart := parentWatchProcessStart(pid)
	if currentStart <= 0 {
		// Cannot verify identity now; preserve the exclude to avoid a
		// false-negative self-kill (safe direction).
		return true
	}
	diff := currentStart - recordedStart
	if diff < 0 {
		diff = -diff
	}
	return diff <= 1
}

func launchChainProcesses() []watchedProcess {
	selfPID := parentWatchCurrentPID()
	parentPID := parentWatchParentPID(selfPID)
	if parentPID <= 0 {
		return nil
	}

	parent := watchedProcess{
		PID:       parentPID,
		Name:      parentWatchProcessName(parentPID),
		StartTime: parentWatchProcessStart(parentPID),
	}
	chain := []watchedProcess{parent}

	if isShellLauncher(parent.Name) {
		if grandparentPID := parentWatchParentPID(parent.PID); grandparentPID > 0 {
			chain = append(chain, watchedProcess{
				PID:       grandparentPID,
				Name:      parentWatchProcessName(grandparentPID),
				StartTime: parentWatchProcessStart(grandparentPID),
			})
		}
	}

	return chain
}

func isShellLauncher(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".exe")
	switch name {
	case "powershell", "pwsh", "cmd":
		return true
	default:
		return false
	}
}

func parentWatchDisabled() bool {
	value := strings.TrimSpace(os.Getenv(disableParentWatchEnv))
	return strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "yes")
}

func formatWatchedProcesses(chain []watchedProcess) []map[string]any {
	out := make([]map[string]any, 0, len(chain))
	for _, proc := range chain {
		out = append(out, map[string]any{
			"pid":  proc.PID,
			"name": proc.Name,
		})
	}
	return out
}
