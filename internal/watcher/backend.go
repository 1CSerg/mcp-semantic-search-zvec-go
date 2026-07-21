package watcher

import (
	"context"
	"runtime"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

type backend interface {
	run(ctx context.Context, settings *config.Settings, events chan<- string, stopCh <-chan struct{}) error
}

func newBackend(settings *config.Settings) (string, backend, error) {
	mode := strings.ToLower(strings.TrimSpace(settings.App.FileWatcher.Backend))
	switch mode {
	case "", "auto":
		if runtime.GOOS == "windows" {
			return "fsnotify", newFSNotifyBackend(), nil
		}
		return "fsnotify", newFSNotifyBackend(), nil
	case "inotify", "fsnotify":
		return mode, newFSNotifyBackend(), nil
	case "polling":
		return "polling", newPollingBackend(), nil
	default:
		return "fsnotify", newFSNotifyBackend(), nil
	}
}
