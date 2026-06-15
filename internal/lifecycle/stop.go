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
	stdioLock := lock.NewStdio(indexDir, defaultLockStaleSeconds)
	if pid, ok := stdioLock.HolderPID(); ok && pid != os.Getpid() {
		if err := terminatePID(pid); err != nil {
			slog.Warn("terminate stdio lock holder failed", "pid", pid, "err", err)
		} else {
			time.Sleep(killGrace)
		}
	}
	if stdioLock.ReclaimStale() {
		slog.Info("reclaimed stale stdio lock", "path", stdioLock.Path())
	} else if _, err := os.Stat(stdioLock.Path()); err == nil {
		_ = os.Remove(stdioLock.Path())
	}

	idxLock := lock.New(indexDir, defaultLockStaleSeconds)
	if idxLock.ReclaimStale() {
		slog.Info("reclaimed stale index lock", "path", idxLock.Path())
	}
	return nil
}
