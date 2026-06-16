package lifecycle

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

const disableParentWatchEnv = "MCP_DISABLE_PARENT_WATCH"

var (
	parentWatchInterval     = 2 * time.Second
	parentWatchCurrentPID   = os.Getpid
	parentWatchParentPID    = defaultParentPID
	parentWatchProcessName  = defaultProcessName
	parentWatchProcessAlive = lock.ProcessAlive
)

type watchedProcess struct {
	PID  int
	Name string
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
				if parentWatchProcessAlive(proc.PID) {
					continue
				}
				slog.Warn("stdio parent exited, shutting down", "pid", proc.PID, "name", proc.Name)
				stop()
				return
			}
		}
	}
}

func launchChainProcesses() []watchedProcess {
	selfPID := parentWatchCurrentPID()
	parentPID := parentWatchParentPID(selfPID)
	if parentPID <= 0 {
		return nil
	}

	parent := watchedProcess{PID: parentPID, Name: parentWatchProcessName(parentPID)}
	chain := []watchedProcess{parent}

	if isShellLauncher(parent.Name) {
		if grandparentPID := parentWatchParentPID(parent.PID); grandparentPID > 0 {
			chain = append(chain, watchedProcess{
				PID:  grandparentPID,
				Name: parentWatchProcessName(grandparentPID),
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
