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

type fileSnapshot struct {
	mtimeNs int64
	size    int64
}

func (b *pollingBackend) run(ctx context.Context, settings *config.Settings, events chan<- string, stopCh <-chan struct{}) error {
	return b.runWithBaselineReady(ctx, settings, events, stopCh, nil)
}

func (b *pollingBackend) runWithBaselineReady(ctx context.Context, settings *config.Settings, events chan<- string, stopCh <-chan struct{}, baselineReady chan struct{}) error {
	interval := time.Duration(settings.App.FileWatcher.PollIntervalSeconds * float64(time.Second))
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	prev, err := snapshot(settings)
	if err != nil {
		prev = nil
	}
	if baselineReady != nil {
		close(baselineReady)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-stopCh:
			return nil
		case <-ticker.C:
			prev, err = b.pollOnce(ctx, settings, events, stopCh, prev)
			if err != nil {
				continue
			}
		}
	}
}

func (b *pollingBackend) pollOnce(ctx context.Context, settings *config.Settings, events chan<- string, stopCh <-chan struct{}, prev map[string]fileSnapshot) (map[string]fileSnapshot, error) {
	curr, err := snapshot(settings)
	if err != nil {
		return prev, err
	}
	if prev != nil {
		for rel, stat := range curr {
			if prevStat, ok := prev[rel]; !ok || prevStat != stat {
				select {
				case events <- rel:
				case <-ctx.Done():
					return prev, nil
				case <-stopCh:
					return prev, nil
				}
			}
		}
		for rel := range prev {
			if _, ok := curr[rel]; !ok {
				select {
				case events <- rel:
				case <-ctx.Done():
					return prev, nil
				case <-stopCh:
					return prev, nil
				}
			}
		}
	}
	return curr, nil
}

func snapshot(settings *config.Settings) (map[string]fileSnapshot, error) {
	scanResult, err := scan.Discover(scan.Options{
		Root:       settings.WorkspaceRoot,
		Extensions: settings.App.Indexing.Extensions,
		SkipDirs:   settings.App.Indexing.SkipDirs,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]fileSnapshot, len(scanResult.Files))
	for _, rel := range scanResult.Files {
		abs := filepath.Join(settings.WorkspaceRoot, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		out[rel] = fileSnapshot{mtimeNs: info.ModTime().UnixNano(), size: info.Size()}
	}
	return out, nil
}
