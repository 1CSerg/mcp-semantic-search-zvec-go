package lifecycle

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
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
	parentWatchOnce         sync.Once
)

type watchedProcess struct {
	PID       int
	Name      string
	StartTime int64
}

// StartParentWatch stops the process when the stdio launch-chain ancestor exits.
// Call at most once per process; subsequent calls are ignored.
func StartParentWatch(ctx context.Context, stop context.CancelFunc) {
	parentWatchOnce.Do(func() {
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
	})
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
				slog.Warn("stdio parent exited, shutting down", "pid", proc.PID, "name", proc.Name)
				stop()
				return
			}
		}
	}
}

func sameProcess(pid int, recordedStart int64) bool {
	if recordedStart <= 0 {
		return true
	}
	currentStart := parentWatchProcessStart(pid)
	if currentStart <= 0 {
		return true
	}
	return currentStart == recordedStart
}

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
		return true
	}
	return currentStart == recordedStart
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
