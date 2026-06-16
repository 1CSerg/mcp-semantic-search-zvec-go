package lifecycle

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

const defaultLockStaleSeconds = 300.0

// StopStdioForWorkspace stops stale --stdio MCP instances and launcher wrappers for workspace,
// then reclaims stdio/index lock files under indexDir. Used by uninstall scripts.
func StopStdioForWorkspace(workspace, indexDir string) ([]int, error) {
	workspace = filepath.Clean(workspace)
	if workspace == "" {
		return nil, fmt.Errorf("workspace path is empty")
	}
	if indexDir == "" {
		indexDir = filepath.Join(workspace, config.DefaultInstallDirName, config.DefaultIndexSubdir)
	}

	stopped, err := stopStaleStdioInstances(workspace, os.Getpid())
	if err != nil {
		return nil, err
	}

	wrapperStopped, err := stopLauncherWrappers(workspace)
	if err != nil {
		return nil, err
	}
	stopped = append(stopped, wrapperStopped...)

	if len(stopped) > 0 {
		time.Sleep(killGrace)
	}

	if err := forceReclaimLocks(indexDir); err != nil {
		slog.Warn("lock reclaim failed", "index_dir", indexDir, "err", err)
	}

	return stopped, nil
}

func forceReclaimLocks(indexDir string) error {
	if indexDir == "" {
		return nil
	}
	// Matching --stdio instances for this workspace were already terminated by
	// stopStaleStdioInstances (identity-checked). Here we only reclaim locks
	// that are provably stale (dead PID or expired) and never blindly kill the
	// recorded PID or remove a lock whose holder is still alive.
	stdioLock := lock.NewStdio(indexDir, defaultLockStaleSeconds)
	if stdioLock.ReclaimStale() {
		slog.Info("reclaimed stale stdio lock", "path", stdioLock.Path())
	} else if pid, ok := stdioLock.LiveHolder(); ok {
		slog.Warn("stdio lock still held by a live process; not removing", "holder_pid", pid, "path", stdioLock.Path())
	}

	idxLock := lock.New(indexDir, defaultLockStaleSeconds)
	if idxLock.ReclaimStale() {
		slog.Info("reclaimed stale index lock", "path", idxLock.Path())
	}
	return nil
}
