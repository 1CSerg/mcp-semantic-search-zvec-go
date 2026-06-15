package lifecycle

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

// IsZvecLockError reports zvec collection LOCK contention errors.
func IsZvecLockError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "lock file") || strings.Contains(msg, "can't open lock")
}

// RecoverDuplicateStdio stops other stdio MCP instances for the workspace.
func RecoverDuplicateStdio(settings *config.Settings) error {
	stopped, err := stopStaleStdioInstances(settings.WorkspaceRoot, os.Getpid())
	if err != nil {
		return err
	}
	for _, pid := range stopped {
		slog.Info("stopped stale stdio during zvec recovery", "pid", pid, "workspace", settings.WorkspaceRoot)
	}
	if len(stopped) > 0 {
		time.Sleep(killGrace)
	}
	return nil
}
