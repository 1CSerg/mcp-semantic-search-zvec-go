package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/fsnotify/fsnotify"
)

type fsnotifyBackend struct{}

func newFSNotifyBackend() backend {
	return &fsnotifyBackend{}
}

func (b *fsnotifyBackend) run(ctx context.Context, settings *config.Settings, events chan<- string, stopCh <-chan struct{}) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("fsnotify init failed", "err", err)
		return fmt.Errorf("fsnotify init: %w", err)
	}
	defer func() { _ = w.Close() }()

	root := settings.WorkspaceRoot
	if err := addWatchTree(w, root, settings.App.Indexing.SkipDirs); err != nil {
		slog.Error("fsnotify add watch failed", "err", err)
		return fmt.Errorf("fsnotify add watch: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-stopCh:
			return nil
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			slog.Warn("fsnotify error", "err", err)
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			rel, err := filepath.Rel(root, ev.Name)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			select {
			case events <- rel:
			case <-ctx.Done():
				return nil
			case <-stopCh:
				return nil
			}
			if ev.Op&fsnotify.Create != 0 {
				if err := addWatchTree(w, ev.Name, settings.App.Indexing.SkipDirs); err != nil {
					slog.Warn("watcher failed to watch new directory", "path", ev.Name, "err", err)
				}
			}
		}
	}
}

func addWatchTree(w *fsnotify.Watcher, root string, skipDirs []string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipDir(filepath.Base(path), skipDirs) {
				return filepath.SkipDir
			}
			return w.Add(path)
		}
		return nil
	})
}
