package lifecycle

import (
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/zvecerr"
)

// IsZvecLockError reports zvec collection LOCK contention errors.
func IsZvecLockError(err error) bool {
	return zvecerr.IsLockError(err)
}

// RecoverDuplicateStdio stops other stdio MCP instances for the workspace.
func RecoverDuplicateStdio(settings *config.Settings) error {
	stopped, err := stopStaleStdioInstances(settings.WorkspaceRoot, os.Getpid())
	if err != nil && !errors.Is(err, ErrStdioScanUnsupported) {
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
