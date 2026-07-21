package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestPollingBackendDetectsChange(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("version-one"), 0o644); err != nil {
		t.Fatal(err)
	}

	settings := &config.Settings{
		WorkspaceRoot: root,
		App: config.AppConfig{
			FileWatcher: config.FileWatcherConfig{
				PollIntervalSeconds: 0.05,
			},
			Indexing: config.IndexingConfig{
				Extensions: []string{".go"},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string, 4)
	baselineReady := make(chan struct{})
	pb := newPollingBackend().(*pollingBackend)
	go pb.runWithBaselineReady(ctx, settings, events, nil, baselineReady)

	select {
	case <-baselineReady:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for polling baseline")
	}

	select {
	case <-events:
		t.Fatal("unexpected event on baseline snapshot")
	default:
	}

	if err := os.WriteFile(file, []byte("version-two-longer"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case rel := <-events:
			if rel != "main.go" {
				t.Fatalf("rel=%q", rel)
			}
			return
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatal("timeout waiting for polling event")
}

func TestPollingBackendDetectsChangeBeforeFirstTick(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("baseline-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	settings := &config.Settings{
		WorkspaceRoot: root,
		App: config.AppConfig{
			FileWatcher: config.FileWatcherConfig{
				PollIntervalSeconds: 2,
			},
			Indexing: config.IndexingConfig{
				Extensions: []string{".go"},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string, 4)
	baselineReady := make(chan struct{})
	pb := newPollingBackend().(*pollingBackend)
	go pb.runWithBaselineReady(ctx, settings, events, nil, baselineReady)

	select {
	case <-baselineReady:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for polling baseline")
	}

	if err := os.WriteFile(file, []byte("changed-before-first-tick-with-distinct-size"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case rel := <-events:
			if rel != "main.go" {
				t.Fatalf("rel=%q", rel)
			}
			return
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatal("timeout waiting for polling event before first tick")
}

func TestWatcherTriggersReindex(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	coord := &mockCoordinator{}
	settings := &config.Settings{
		WorkspaceRoot: root,
		App: config.AppConfig{
			FileWatcher: config.FileWatcherConfig{
				Enabled:             true,
				Backend:             "polling",
				DebounceSeconds:     0.05,
				PollIntervalSeconds: 0.05,
			},
			Indexing: config.IndexingConfig{
				Extensions: []string{".go"},
			},
		},
	}
	w, err := New(settings, coord)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	w.PrepareRun()
	go w.Start(ctx)

	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(file, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if coord.Starts() > 0 {
			st := w.Snapshot()
			if st.Backend != "polling" {
				t.Fatalf("backend=%q", st.Backend)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("starts=%d", coord.Starts())
}

func TestNewBackendPolling(t *testing.T) {
	name, b, err := newBackend(&config.Settings{App: config.AppConfig{FileWatcher: config.FileWatcherConfig{Backend: "polling"}}})
	if err != nil {
		t.Fatal(err)
	}
	if name != "polling" || b == nil {
		t.Fatalf("name=%q backend=%v", name, b)
	}
}

func TestNewBackendInotify(t *testing.T) {
	name, _, err := newBackend(&config.Settings{App: config.AppConfig{FileWatcher: config.FileWatcherConfig{Backend: "inotify"}}})
	if err != nil || name != "inotify" {
		t.Fatalf("name=%q err=%v", name, err)
	}
}

func TestNewBackendUnknown(t *testing.T) {
	name, _, err := newBackend(&config.Settings{App: config.AppConfig{FileWatcher: config.FileWatcherConfig{Backend: "unknown"}}})
	if err != nil || name != "fsnotify" {
		t.Fatalf("name=%q err=%v", name, err)
	}
}

func TestNewBackendAuto(t *testing.T) {
	name, _, err := newBackend(&config.Settings{App: config.AppConfig{FileWatcher: config.FileWatcherConfig{Backend: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	if name != "fsnotify" {
		t.Fatalf("backend=%q", name)
	}
}
