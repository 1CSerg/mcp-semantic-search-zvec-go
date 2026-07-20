package lifecycle

import (
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

// PrepareWorkspaceLocks stops stale MCP stdio instances, reclaims orphaned lock files,
// and heals interrupted indexing progress before opening the zvec collection.
func PrepareWorkspaceLocks(settings *config.Settings) error {
	if settings == nil {
		return nil
	}

	stopped, err := stopStaleStdioInstances(settings.WorkspaceRoot, os.Getpid())
	if err != nil && !errors.Is(err, ErrStdioScanUnsupported) {
		return err
	}
	for _, pid := range stopped {
		slog.Info("stopped stale stdio mcp process", "pid", pid, "workspace", settings.WorkspaceRoot)
	}

	wrapperStopped, err := stopLauncherWrappers(settings.WorkspaceRoot, launchChainPIDSet())
	if err != nil {
		return err
	}
	for _, pid := range wrapperStopped {
		slog.Info("stopped stale launcher wrapper", "pid", pid, "workspace", settings.WorkspaceRoot)
	}
	stopped = append(stopped, wrapperStopped...)

	if len(stopped) > 0 {
		time.Sleep(killGrace)
	}

	staleSecs := settings.App.Indexing.LockStaleSeconds
	reclaimIndexLocks(settings.IndexDir, staleSecs)
	reclaimWorkspaceZvecLock(settings)

	if err := indexer.RecoverStalledProgress(settings.IndexDir, settings.App.Indexing.StallSeconds, func() bool {
		_, ok := FindStdioForWorkspace(settings.WorkspaceRoot, os.Getpid())
		return ok
	}); err != nil {
		return err
	}
	return indexer.RecoverInterruptedProgress(settings.IndexDir)
}

func reclaimIndexLocks(indexDir string, staleSecs float64) {
	if indexDir == "" {
		return
	}
	idxLock := lock.New(indexDir, staleSecs)
	if idxLock.ReclaimStale() {
		slog.Info("reclaimed stale index lock", "path", idxLock.Path())
	}
	stdioLock := lock.NewStdio(indexDir, staleSecs)
	if stdioLock.ReclaimStale() {
		slog.Info("reclaimed stale stdio lock", "path", stdioLock.Path())
	}
}

func reclaimWorkspaceZvecLock(settings *config.Settings) {
	if settings == nil {
		return
	}
	if cfg, ok := zvecConfigFromSettings(settings); ok {
		if zvec.ReclaimCollectionLock(cfg) {
			slog.Info("reclaimed orphaned zvec collection lock", "collection", zvec.CollectionPath(cfg))
		}
		return
	}
	if n := zvec.ReclaimAllCollectionLocks(settings.IndexDir); n > 0 {
		slog.Info("reclaimed orphaned zvec collection locks", "count", n, "index_dir", settings.IndexDir)
	}
}

func zvecConfigFromSettings(settings *config.Settings) (zvec.Config, bool) {
	profile, err := settings.ActiveProfile()
	if err != nil {
		return zvec.Config{}, false
	}
	return zvec.Config{
		IndexDir:      settings.IndexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}, true
}
