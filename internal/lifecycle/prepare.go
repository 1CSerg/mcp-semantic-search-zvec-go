package lifecycle

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
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
		if err := PrepareWorkspaceLocks(settings); err != nil {
			return nil, err
		}

		acquireErr := stdioLock.TryAcquire()
		if acquireErr == nil {
			return stdioLock, nil
		}
		lastErr = acquireErr
		if pid, ok := stdioLock.LiveHolder(); ok {
			slog.Warn("stdio lock held by another process", "holder_pid", pid, "attempt", attempt+1)
		} else {
			slog.Warn("stdio lock acquire failed", "err", acquireErr, "attempt", attempt+1)
		}
		if attempt < stdioLockRetries-1 {
			time.Sleep(stdioLockRetryDelay)
		}
	}
	return nil, fmt.Errorf("another MCP stdio instance is running for this workspace: %w", lastErr)
}
