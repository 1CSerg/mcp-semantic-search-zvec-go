package lifecycle

import (
	"log/slog"
	"os"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

const killGrace = 300 * time.Millisecond

// PrepareStdio ensures log dir exists and stops stale stdio MCP processes for the workspace.
func PrepareStdio(settings *config.Settings) error {
	if err := os.MkdirAll(settings.LogsDir(), 0o755); err != nil {
		return err
	}
	stopped, err := stopStaleStdioInstances(settings.WorkspaceRoot, os.Getpid())
	if err != nil {
		return err
	}
	for _, pid := range stopped {
		slog.Info("stopped stale stdio mcp process", "pid", pid, "workspace", settings.WorkspaceRoot)
	}
	l := lock.New(settings.IndexDir, settings.App.Indexing.LockStaleSeconds)
	if l.ReclaimStale() {
		slog.Info("reclaimed stale index lock", "path", l.Path())
	}
	if err := indexer.RecoverStalledProgress(settings.IndexDir, settings.App.Indexing.StallSeconds); err != nil {
		return err
	}
	return nil
}
