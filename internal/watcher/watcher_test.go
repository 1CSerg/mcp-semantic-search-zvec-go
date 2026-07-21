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
	mu      sync.Mutex
	running bool
	starts  int
}

func (m *mockCoordinator) Start(bool) (indexer.Progress, error) {
	m.mu.Lock()
	m.starts++
	m.running = true
	m.mu.Unlock()
	go func() {
		time.Sleep(50 * time.Millisecond)
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()
	return indexer.StartRunning(false), nil
}

func (m *mockCoordinator) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *mockCoordinator) Starts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.starts
}

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
	w.PrepareRun()
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

func (b *failingBackend) run(_ context.Context, _ *config.Settings, _ chan<- string, stop <-chan struct{}) error {
	_ = stop
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

func (d *delayedCoordinator) Starts() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.starts
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w.PrepareRun()
	go w.Start(ctx)

	time.Sleep(300 * time.Millisecond)
	if err := os.WriteFile(file, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if coord.Starts() >= 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("starts=%d pending=%d", coord.Starts(), w.Snapshot().PendingEvents)
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

func (t *toggleCoordinator) Starts() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.starts
}

func (t *toggleCoordinator) setIdleAfter(d time.Duration) {
	time.Sleep(d)
	t.mu.Lock()
	t.running = false
	t.mu.Unlock()
}

func TestRunRetryLoop(t *testing.T) {
	coord := &toggleCoordinator{running: true}
	w := &Watcher{
		settings:    &config.Settings{},
		coordinator: coord,
		stopCh:      make(chan struct{}),
		retryActive: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go coord.setIdleAfter(400 * time.Millisecond)
	w.runRetryLoop(ctx)
	if coord.Starts() == 0 {
		t.Fatal("expected reindex after coordinator became idle")
	}
}

func TestWatcherTriggerReindexRetryPendingWhileActive(t *testing.T) {
	coord := &toggleCoordinator{running: true}
	w := &Watcher{
		settings:    &config.Settings{},
		coordinator: coord,
		stopCh:      make(chan struct{}),
		retryActive: true,
	}
	w.triggerReindex(context.Background(), "main.go")
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.retryPending {
		t.Fatal("expected retry pending")
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
		if coord.Starts() > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("starts=%d", coord.Starts())
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

type alreadyRunningCoordinator struct{}

func (alreadyRunningCoordinator) Start(bool) (indexer.Progress, error) {
	return indexer.Progress{}, indexer.ErrAlreadyRunning
}

func (alreadyRunningCoordinator) IsRunning() bool { return true }

func TestWatcherAlreadyRunningNotLastError(t *testing.T) {
	w := &Watcher{
		settings:    &config.Settings{},
		coordinator: alreadyRunningCoordinator{},
		stopCh:      make(chan struct{}),
	}
	w.startReindex(context.Background(), "main.go")
	st := w.Snapshot()
	if st.LastError != "" {
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
	if coord.Starts() != 0 {
		t.Fatalf("starts=%d", coord.Starts())
	}
	w.triggerReindex(ctx, "pkg/main.go")
	if coord.Starts() != 1 {
		t.Fatalf("starts=%d", coord.Starts())
	}
}

func TestWatcherLoopTriggersReindexOnDirectoryRemove(t *testing.T) {
	coord := &mockCoordinator{}
	w := &Watcher{
		settings: &config.Settings{
			App: config.AppConfig{
				FileWatcher: config.FileWatcherConfig{
					DebounceSeconds: 0.05,
				},
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
	events := make(chan string, 1)
	go w.loop(ctx, events)
	// fsnotify often emits only the directory path on recursive delete.
	events <- "pkg"
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if coord.Starts() >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("starts=%d, expected reindex after directory remove", coord.Starts())
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
