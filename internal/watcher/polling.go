package watcher

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/scan"
)

type pollingBackend struct{}

func newPollingBackend() backend {
	return &pollingBackend{}
}

func (b *pollingBackend) run(ctx context.Context, settings *config.Settings, events chan<- string) error {
	interval := time.Duration(settings.App.FileWatcher.PollIntervalSeconds * float64(time.Second))
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var prev map[string]int64
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			curr, err := snapshot(settings)
			if err != nil {
				continue
			}
			if prev != nil {
				for rel, mtime := range curr {
					if prev[rel] != mtime {
						select {
						case events <- rel:
						case <-ctx.Done():
							return nil
						}
					}
				}
				for rel := range prev {
					if _, ok := curr[rel]; !ok {
						select {
						case events <- rel:
						case <-ctx.Done():
							return nil
						}
					}
				}
			}
			prev = curr
		}
	}
}

func snapshot(settings *config.Settings) (map[string]int64, error) {
	scanResult, err := scan.Discover(scan.Options{
		Root:       settings.WorkspaceRoot,
		Extensions: settings.App.Indexing.Extensions,
		SkipDirs:   settings.App.Indexing.SkipDirs,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(scanResult.Files))
	for _, rel := range scanResult.Files {
		abs := filepath.Join(settings.WorkspaceRoot, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		out[rel] = info.ModTime().UnixNano()
	}
	return out, nil
}
