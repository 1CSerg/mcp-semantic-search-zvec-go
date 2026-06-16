package lifecycle

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

const (
	killGrace           = 300 * time.Millisecond
	stdioLockRetries    = 3
	stdioLockRetryDelay = 400 * time.Millisecond
)

// PrepareStdio ensures log dir exists, stops stale stdio MCP processes, and acquires stdio.lock.
// The caller must Release the returned lock on shutdown.
func PrepareStdio(settings *config.Settings) (*lock.Lock, error) {
	if err := os.MkdirAll(settings.LogsDir(), 0o755); err != nil {
		return nil, err
	}

	staleSecs := settings.App.Indexing.LockStaleSeconds
	stdioLock := lock.NewStdio(settings.IndexDir, staleSecs)

	var lastErr error
	for attempt := 0; attempt < stdioLockRetries; attempt++ {
		stopped, err := stopStaleStdioInstances(settings.WorkspaceRoot, os.Getpid())
		if err != nil {
			return nil, err
		}
		for _, pid := range stopped {
			slog.Info("stopped stale stdio mcp process", "pid", pid, "workspace", settings.WorkspaceRoot)
		}
		if len(stopped) > 0 {
			time.Sleep(killGrace)
		}

		idxLock := lock.New(settings.IndexDir, staleSecs)
		if idxLock.ReclaimStale() {
			slog.Info("reclaimed stale index lock", "path", idxLock.Path())
		}
		if stdioLock.ReclaimStale() {
			slog.Info("reclaimed stale stdio lock", "path", stdioLock.Path())
		}
		if err := indexer.RecoverStalledProgress(settings.IndexDir, settings.App.Indexing.StallSeconds, func() bool {
			_, ok := FindStdioForWorkspace(settings.WorkspaceRoot, os.Getpid())
			return ok
		}); err != nil {
			return nil, err
		}

		if err := stdioLock.TryAcquire(); err == nil {
			return stdioLock, nil
		}
		lastErr = err
		if pid, ok := stdioLock.LiveHolder(); ok {
			slog.Warn("stdio lock held by another process", "holder_pid", pid, "attempt", attempt+1)
		} else {
			slog.Warn("stdio lock acquire failed", "err", err, "attempt", attempt+1)
		}
		if attempt < stdioLockRetries-1 {
			time.Sleep(stdioLockRetryDelay)
		}
	}
	return nil, fmt.Errorf("another MCP stdio instance is running for this workspace: %w", lastErr)
}
