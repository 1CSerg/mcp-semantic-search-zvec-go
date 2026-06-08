package watcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
)

type mockCoordinator struct {
	running bool
	starts  int
}

func (m *mockCoordinator) Start(bool) (indexer.Progress, error) {
	m.starts++
	m.running = true
	go func() {
		time.Sleep(50 * time.Millisecond)
		m.running = false
	}()
	return indexer.StartRunning(false), nil
}

func (m *mockCoordinator) IsRunning() bool { return m.running }

func TestWatcherDisabled(t *testing.T) {
	w, err := New(&config.Settings{App: config.AppConfig{FileWatcher: config.FileWatcherConfig{Enabled: false}}}, &mockCoordinator{})
	if err != nil {
		t.Fatal(err)
	}
	if w != nil {
		t.Fatal("expected nil watcher")
	}
}

func TestWatcherDaemonUnsupported(t *testing.T) {
	w, err := New(&config.Settings{
		App: config.AppConfig{
			FileWatcher: config.FileWatcherConfig{Enabled: true, RunAsDaemon: true},
		},
	}, &mockCoordinator{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	st := w.Snapshot()
	if st.Running {
		t.Fatal("daemon watcher should not run")
	}
	if st.LastError == "" {
		t.Fatal("expected unsupported error")
	}
}

func TestWatcherBackendFailure(t *testing.T) {
	coord := &mockCoordinator{}
	settings := &config.Settings{
		WorkspaceRoot: t.TempDir(),
		App: config.AppConfig{
			FileWatcher: config.FileWatcherConfig{
				Enabled: true,
				Backend: "polling",
			},
		},
	}
	w, err := New(settings, coord)
	if err != nil {
		t.Fatal(err)
	}
	w.backend = &failingBackend{err: errors.New("backend init failed")}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := w.Snapshot()
		if !st.Running && st.LastError == "backend init failed" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := w.Snapshot()
	t.Fatalf("running=%v last_error=%q", st.Running, st.LastError)
}

type failingBackend struct {
	err error
}

func (b *failingBackend) run(context.Context, *config.Settings, chan<- string) error {
	return b.err
}

type delayedCoordinator struct {
	mu      sync.Mutex
	running bool
	starts  int
}

func (d *delayedCoordinator) Start(bool) (indexer.Progress, error) {
	d.mu.Lock()
	d.starts++
	d.running = true
	d.mu.Unlock()
	go func() {
		time.Sleep(700 * time.Millisecond)
		d.mu.Lock()
		d.running = false
		d.mu.Unlock()
	}()
	return indexer.StartRunning(false), nil
}

func (d *delayedCoordinator) IsRunning() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

type errorCoordinator struct{}

func (errorCoordinator) Start(bool) (indexer.Progress, error) {
	return indexer.Progress{}, errors.New("start failed")
}

func (errorCoordinator) IsRunning() bool { return false }

func TestWatcherWaitAndRetry(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	coord := &delayedCoordinator{}
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
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	go w.Start(ctx)

	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(file, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if coord.starts >= 1 {
			st := w.Snapshot()
			if st.PendingEvents == 0 && coord.starts >= 1 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("starts=%d pending=%d", coord.starts, w.Snapshot().PendingEvents)
}

type toggleCoordinator struct {
	mu      sync.Mutex
	running bool
	starts  int
}

func (t *toggleCoordinator) Start(bool) (indexer.Progress, error) {
	t.mu.Lock()
	t.starts++
	t.mu.Unlock()
	return indexer.StartRunning(false), nil
}

func (t *toggleCoordinator) IsRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

func (t *toggleCoordinator) setIdleAfter(d time.Duration) {
	time.Sleep(d)
	t.mu.Lock()
	t.running = false
	t.mu.Unlock()
}

func TestWaitAndRetry(t *testing.T) {
	coord := &toggleCoordinator{running: true}
	w := &Watcher{
		settings:    &config.Settings{},
		coordinator: coord,
		stopCh:      make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go coord.setIdleAfter(400 * time.Millisecond)
	w.waitAndRetry(ctx, "main.go")
	if coord.starts == 0 {
		t.Fatal("expected reindex after coordinator became idle")
	}
}

func TestWatcherTriggerReindexWhileRunning(t *testing.T) {
	coord := &delayedCoordinator{}
	w := &Watcher{
		settings:    &config.Settings{},
		coordinator: coord,
		stopCh:      make(chan struct{}),
	}
	coord.mu.Lock()
	coord.running = true
	coord.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go coord.setIdleAfter(600 * time.Millisecond)
	w.triggerReindex(ctx, "main.go")

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if coord.starts > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("starts=%d", coord.starts)
}

func (d *delayedCoordinator) setIdleAfter(delay time.Duration) {
	time.Sleep(delay)
	d.mu.Lock()
	d.running = false
	d.mu.Unlock()
}

func TestWatcherTriggerReindexError(t *testing.T) {
	w := &Watcher{
		settings:    &config.Settings{},
		coordinator: errorCoordinator{},
		stopCh:      make(chan struct{}),
	}
	w.triggerReindex(context.Background(), "main.go")
	st := w.Snapshot()
	if st.LastError != "start failed" {
		t.Fatalf("last_error=%q", st.LastError)
	}
}

func TestWatcherLoopSkipsIrrelevantFiles(t *testing.T) {
	coord := &mockCoordinator{}
	w := &Watcher{
		settings: &config.Settings{
			App: config.AppConfig{
				Indexing: config.IndexingConfig{
					Extensions: []string{".go"},
					SkipDirs:   []string{".git"},
				},
			},
		},
		coordinator: coord,
		stopCh:      make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string, 2)
	go w.loop(ctx, events)
	events <- "readme.txt"
	events <- "pkg/main.go"
	time.Sleep(100 * time.Millisecond)
	if coord.starts != 0 {
		t.Fatalf("starts=%d", coord.starts)
	}
	w.triggerReindex(ctx, "pkg/main.go")
	if coord.starts != 1 {
		t.Fatalf("starts=%d", coord.starts)
	}
}

func TestWatcherSnapshot(t *testing.T) {
	settings := &config.Settings{
		WorkspaceRoot: t.TempDir(),
		App: config.AppConfig{
			FileWatcher: config.FileWatcherConfig{
				Enabled:         true,
				Backend:         "polling",
				DebounceSeconds: 0.05,
			},
			Indexing: config.IndexingConfig{
				Extensions: []string{".go"},
				SkipDirs:   []string{".git"},
			},
		},
	}
	coord := &mockCoordinator{}
	w, err := New(settings, coord)
	if err != nil {
		t.Fatal(err)
	}
	st := w.Snapshot()
	if !st.EnabledInConfig || st.Backend != "polling" {
		t.Fatalf("status=%+v", st)
	}
}
