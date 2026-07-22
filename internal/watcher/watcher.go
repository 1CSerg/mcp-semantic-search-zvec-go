package watcher

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/scan"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/redact"
)

// Coordinator triggers background indexing.
type Coordinator interface {
	Start(force bool) (indexer.Progress, error)
	IsRunning() bool
}

// Status is exposed via index_status.
type Status struct {
	EnabledInConfig bool   `json:"enabled_in_config"`
	Running         bool   `json:"running"`
	Backend         string `json:"backend,omitempty"`
	RunAsDaemon     bool   `json:"run_as_daemon"`
	DaemonSupported bool   `json:"daemon_supported"`
	LastEventAt     string `json:"last_event_at,omitempty"`
	LastReindexAt   string `json:"last_reindex_at,omitempty"`
	PendingEvents   int    `json:"pending_events,omitempty"`
	LastError       string `json:"last_error,omitempty"`
}

// Watcher debounces filesystem events and triggers incremental reindex.
type Watcher struct {
	settings    *config.Settings
	coordinator Coordinator
	backendName string
	backend     backend

	mu            sync.Mutex
	running       bool
	lastEventAt   time.Time
	lastReindexAt time.Time
	pendingEvents int
	lastError     string
	debounce      *time.Timer
	stopCh        chan struct{}
	stopOnce      sync.Once
	wg            sync.WaitGroup // tracks loop + backend + retry goroutines so Stop can wait
	debounceWg    sync.WaitGroup // tracks in-flight debounce callbacks
	retryActive   bool
	retryPending  bool
	lastRetryRel  string
}

// New creates a watcher when enabled in config.
func New(settings *config.Settings, coordinator Coordinator) (*Watcher, error) {
	cfg := settings.App.FileWatcher
	if !cfg.Enabled || coordinator == nil {
		return nil, nil
	}
	if cfg.RunAsDaemon {
		return &Watcher{
			settings:    settings,
			coordinator: coordinator,
			backendName: "daemon",
		}, nil
	}
	backendName, impl, err := newBackend(settings)
	if err != nil {
		return nil, err
	}
	return &Watcher{
		settings:    settings,
		coordinator: coordinator,
		backendName: backendName,
		backend:     impl,
		stopCh:      make(chan struct{}),
	}, nil
}

// PrepareRun registers goroutines with the WaitGroup. Call before go Start(ctx).
func (w *Watcher) PrepareRun() {
	if w == nil || w.backend == nil {
		return
	}
	w.wg.Add(2)
}

// Start runs the watcher until ctx is cancelled.
func (w *Watcher) Start(ctx context.Context) {
	if w == nil {
		return
	}
	if w.settings.App.FileWatcher.RunAsDaemon {
		w.mu.Lock()
		w.running = false
		w.lastError = "run_as_daemon is not supported"
		w.mu.Unlock()
		return
	}
	if w.backend == nil {
		return
	}
	w.mu.Lock()
	w.running = true
	w.mu.Unlock()

	events := make(chan string, 64)
	go func() {
		defer w.wg.Done()
		err := w.backend.run(ctx, w.settings, events, w.stopCh)
		close(events)
		if err != nil && ctx.Err() == nil && !w.isStopped() {
			w.mu.Lock()
			w.running = false
			w.lastError = err.Error()
			w.mu.Unlock()
			slog.Error("watcher backend stopped", "backend", w.backendName, "err", err)
		}
	}()
	go func() {
		defer w.wg.Done()
		w.loop(ctx, events)
	}()

	<-ctx.Done()
	w.finishStop()
}

// isStopped reports whether Stop (or finishStop) has closed stopCh.
func (w *Watcher) isStopped() bool {
	select {
	case <-w.stopCh:
		return true
	default:
		return false
	}
}

// finishStop performs the idempotent teardown shared by the Start exit path and
// Stop: it marks the watcher not-running, stops the debounce timer and closes
// stopCh exactly once so all select-loops listening on it unblock.
func (w *Watcher) finishStop() {
	w.mu.Lock()
	w.running = false
	if w.debounce != nil {
		if w.debounce.Stop() {
			w.debounceWg.Done()
		}
		w.debounce = nil
	}
	w.mu.Unlock()
	w.stopOnce.Do(func() { close(w.stopCh) })
	w.debounceWg.Wait()
}

// Stop cancels the watcher's event loop without depending on the caller's
// context. It is safe to call concurrently and multiple times. After Stop the
// watcher will not trigger any new reindex jobs, which lets a service shutdown
// safely release native resources (tree-sitter handles, zvec) without racing
// the watcher. Stop waits for the loop and backend goroutines to exit.
func (w *Watcher) Stop() {
	if w == nil || w.backend == nil {
		return
	}
	if w.isStopped() {
		return
	}
	w.finishStop()
	w.wg.Wait()
}

func (w *Watcher) loop(ctx context.Context, events <-chan string) {
	debounce := time.Duration(w.settings.App.FileWatcher.DebounceSeconds * float64(time.Second))
	if debounce <= 0 {
		debounce = 2 * time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case rel, ok := <-events:
			if !ok {
				return
			}
			if !scan.MatchesWatchPath(rel, w.settings.App.Indexing.Extensions, w.settings.App.Indexing.SkipDirs) {
				continue
			}
			w.noteEvent()
			w.scheduleDebounce(ctx, debounce, rel)
		}
	}
}

func (w *Watcher) scheduleDebounce(ctx context.Context, debounce time.Duration, rel string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pendingEvents++
	if w.debounce != nil {
		if w.debounce.Stop() {
			w.debounceWg.Done()
		}
	}
	w.debounceWg.Add(1)
	w.debounce = time.AfterFunc(debounce, func() {
		defer w.debounceWg.Done()
		w.triggerReindex(ctx, rel)
	})
}

func (w *Watcher) noteEvent() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastEventAt = time.Now().UTC()
}

func (w *Watcher) triggerReindex(ctx context.Context, rel string) {
	select {
	case <-ctx.Done():
		return
	case <-w.stopCh:
		return
	default:
	}

	w.mu.Lock()
	w.pendingEvents = 0
	w.mu.Unlock()

	if w.coordinator.IsRunning() {
		w.mu.Lock()
		w.pendingEvents = 1
		if w.retryActive {
			w.retryPending = true
			w.mu.Unlock()
			return
		}
		w.retryActive = true
		w.lastRetryRel = rel
		w.mu.Unlock()
		w.wg.Add(1)
		go w.runRetryLoop(ctx)
		return
	}
	w.startReindex(ctx, rel)
}

func (w *Watcher) runRetryLoop(ctx context.Context) {
	defer w.wg.Done()
	defer func() {
		w.mu.Lock()
		w.retryActive = false
		w.mu.Unlock()
	}()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			if w.coordinator.IsRunning() {
				continue
			}
			w.mu.Lock()
			w.retryPending = false
			rel := w.lastRetryRel
			w.mu.Unlock()
			w.startReindex(ctx, rel)
			w.mu.Lock()
			needAgain := w.retryPending
			w.mu.Unlock()
			if !needAgain {
				return
			}
		}
	}
}

func (w *Watcher) startReindex(ctx context.Context, rel string) {
	if err := ctx.Err(); err != nil {
		return
	}
	select {
	case <-w.stopCh:
		return
	default:
	}
	if _, err := w.coordinator.Start(false); err != nil {
		if errors.Is(err, indexer.ErrAlreadyRunning) {
			return
		}
		w.mu.Lock()
		w.lastError = err.Error()
		w.mu.Unlock()
		if rel != "" {
			slog.Warn("watcher reindex failed", "file", rel, "err", err)
		} else {
			slog.Warn("watcher reindex failed", "err", err)
		}
		return
	}
	w.mu.Lock()
	w.lastReindexAt = time.Now().UTC()
	w.lastError = ""
	w.mu.Unlock()
	if rel != "" {
		slog.Info("watcher triggered incremental reindex", "file", rel)
	} else {
		slog.Info("watcher triggered incremental reindex")
	}
}

// Snapshot returns current watcher status.
func (w *Watcher) Snapshot() Status {
	if w == nil {
		return Status{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	st := Status{
		EnabledInConfig: w.settings.App.FileWatcher.Enabled,
		Running:         w.running,
		Backend:         w.backendName,
		RunAsDaemon:     w.settings.App.FileWatcher.RunAsDaemon,
		DaemonSupported: false,
		PendingEvents:   w.pendingEvents,
		LastError:       redact.SanitizeErrorText(w.lastError),
	}
	if !w.lastEventAt.IsZero() {
		st.LastEventAt = w.lastEventAt.Format(time.RFC3339)
	}
	if !w.lastReindexAt.IsZero() {
		st.LastReindexAt = w.lastReindexAt.Format(time.RFC3339)
	}
	return st
}
