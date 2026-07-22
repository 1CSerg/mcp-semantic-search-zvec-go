package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

const defaultCloseDrainTimeout = 30 * time.Second

// ErrUnknownWorkspace is returned when workspace_id is not registered.
var ErrUnknownWorkspace = errors.New("unknown workspace")

// ErrRegistryClosing is returned when the registry is shutting down.
var ErrRegistryClosing = errors.New("registry is closing")

// WorkspaceInfo is returned by GET /v1/workspaces.
// Path fields are omitted unless include_paths is requested.
type WorkspaceInfo struct {
	ID         string `json:"id"`
	Open       bool   `json:"open"`
	Root       string `json:"root,omitempty"`
	IndexDir   string `json:"index_dir,omitempty"`
	ConfigPath string `json:"config_path,omitempty"`
}

type workspaceHandle struct {
	spec     WorkspaceSpec
	settings *config.Settings
	svc      service.Service
	phase1   *service.Phase1
	cancel   context.CancelFunc
	lastUsed time.Time
	refs     int
}

type openWait struct {
	done chan struct{}
	err  error
}

// Registry manages lazy-open workspace services with LRU eviction.
type Registry struct {
	cfg         Config
	rootCtx     context.Context
	closeCtx    context.Context
	closeCancel context.CancelFunc

	mu                  sync.Mutex
	open                map[string]*workspaceHandle
	opening             map[string]*openWait
	closing             bool
	discards            int
	runtimeShutdownDone bool
	closeDrainTimeout   time.Duration
	openingNotify       func(workspaceID string)
	onRuntimeShutdown   func() // test hook; called once after CloseResources + ShutdownRuntime
}

// NewRegistry creates a workspace registry from daemon config.
func NewRegistry(cfg Config, rootCtx context.Context) *Registry {
	if rootCtx == nil {
		rootCtx = context.Background()
	}
	closeCtx, closeCancel := context.WithCancel(rootCtx)
	return &Registry{
		cfg:         cfg,
		rootCtx:     rootCtx,
		closeCtx:    closeCtx,
		closeCancel: closeCancel,
		open:        make(map[string]*workspaceHandle),
		opening:     make(map[string]*openWait),
	}
}

// ListWorkspaces returns registered workspace metadata.
// When includePaths is false, only id and open are populated.
func (r *Registry) ListWorkspaces(includePaths bool) []WorkspaceInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]WorkspaceInfo, 0, len(r.cfg.Workspaces))
	for _, spec := range r.cfg.Workspaces {
		_, isOpen := r.open[spec.ID]
		info := WorkspaceInfo{
			ID:   spec.ID,
			Open: isOpen,
		}
		if includePaths {
			info.Root = spec.Root
			info.IndexDir = spec.IndexDir
			info.ConfigPath = spec.ConfigPath
		}
		out = append(out, info)
	}
	return out
}

// GetService opens the workspace if needed and returns its service without holding a borrow.
// Use BorrowService for request-scoped work so LRU eviction waits for in-flight handlers.
func (r *Registry) GetService(workspaceID string) (service.Service, error) {
	svc, release, err := r.BorrowService(workspaceID)
	if err != nil {
		return nil, err
	}
	release()
	return svc, nil
}

// BorrowService returns a workspace service and a release callback.
// Call release when the request finishes so LRU eviction can wait for in-flight work.
func (r *Registry) BorrowService(workspaceID string) (service.Service, func(), error) {
	if workspaceID == "" {
		return nil, nil, fmt.Errorf("workspace_id is required")
	}
	spec := r.cfg.SpecByID(workspaceID)
	if spec == nil {
		return nil, nil, fmt.Errorf("%w: %q", ErrUnknownWorkspace, workspaceID)
	}

	for {
		r.mu.Lock()
		if r.closing {
			r.mu.Unlock()
			return nil, nil, ErrRegistryClosing
		}
		if h, ok := r.open[workspaceID]; ok {
			return r.borrowHandleLocked(h, workspaceID)
		}
		if wait, ok := r.opening[workspaceID]; ok {
			r.mu.Unlock()
			<-wait.done
			if wait.err != nil {
				return nil, nil, wait.err
			}
			continue
		}

		evicted, err := r.reserveOpenSlotLocked(*spec)
		if err != nil {
			r.mu.Unlock()
			return nil, nil, err
		}
		wait := &openWait{done: make(chan struct{})}
		r.opening[workspaceID] = wait
		notify := r.openingNotify
		r.mu.Unlock()
		if notify != nil {
			notify(workspaceID)
		}
		if evicted != nil {
			r.beginDiscard()
			r.discardHandle(evicted)
		}

		var h *workspaceHandle
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					err = fmt.Errorf("workspace init panic: %v", rec)
					slog.Error("workspace init panic", "workspace_id", workspaceID, "panic", rec)
				}
			}()
			h, err = r.initWorkspace(*spec)
		}()
		r.mu.Lock()
		delete(r.opening, workspaceID)
		if r.closing {
			if err != nil {
				wait.err = err
			} else {
				wait.err = ErrRegistryClosing
			}
			close(wait.done)
			if err == nil && h != nil {
				r.discards++
				r.mu.Unlock()
				r.discardHandle(h)
			} else {
				r.mu.Unlock()
			}
			return nil, nil, wait.err
		}
		wait.err = err
		if err == nil {
			r.open[workspaceID] = h
			slog.Info("workspace opened", "workspace_id", spec.ID, "root", spec.Root)
		}
		close(wait.done)
		if err != nil {
			r.mu.Unlock()
			return nil, nil, err
		}
		return r.borrowHandleLocked(h, workspaceID)
	}
}

func (r *Registry) reserveOpenSlotLocked(spec WorkspaceSpec) (*workspaceHandle, error) {
	if _, ok := r.open[spec.ID]; ok {
		return nil, nil
	}
	if _, ok := r.opening[spec.ID]; ok {
		return nil, nil
	}
	occupied := len(r.open) + len(r.opening)
	if occupied >= r.cfg.MaxOpenWorkspaces {
		evicted := r.evictOldestIdleLocked()
		if evicted == nil {
			return nil, fmt.Errorf("max open workspaces (%d) reached and all are in use", r.cfg.MaxOpenWorkspaces)
		}
		return evicted, nil
	}
	return nil, nil
}

func (r *Registry) initWorkspace(spec WorkspaceSpec) (*workspaceHandle, error) {
	if err := r.closeCtx.Err(); err != nil {
		return nil, err
	}
	settings, err := config.LoadWorkspaceFromSpec(spec.ID, spec.Root, spec.IndexDir, spec.ConfigPath, config.LoadOptions{
		PathContainment: config.ParsePathContainmentMode(r.cfg.PathContainment),
		PathAllowlist:   r.cfg.PathAllowlist,
	})
	if err != nil {
		return nil, err
	}
	if err := r.closeCtx.Err(); err != nil {
		return nil, err
	}

	profile, err := settings.ActiveProfile()
	if err == nil {
		cfg := zvec.Config{
			IndexDir:      settings.IndexDir,
			WorkspaceRoot: settings.WorkspaceRoot,
			ProfileName:   settings.App.ActiveProfile,
			Dimensions:    profile.Dimensions,
		}
		zvec.ReclaimCollectionLock(cfg)
	}

	phase1, err := service.NewPhase1(settings)
	if err != nil {
		return nil, err
	}
	wsCtx, cancel := context.WithCancel(r.rootCtx)
	phase1.SetLifecycleContext(wsCtx)

	// If we panic (or return an error) after this point, the caller's recover()
	// wrapper in BorrowService will catch it but won't tear down the partially
	// built phase1 — the watcher goroutine and zvec handles would leak. Install
	// a cleanup guard that fires only on the failure paths; on success the guard
	// is disarmed right before returning the handle.
	success := false
	defer func() {
		if success {
			return
		}
		cancel()
		if closeErr := phase1.Close(); closeErr != nil {
			slog.Warn("phase1 cleanup after failed init", "workspace_id", spec.ID, "err", closeErr)
		}
	}()

	phase1.StartFileWatcher(wsCtx)
	phase1.PrepareStartup()

	success = true
	return &workspaceHandle{
		spec:     spec,
		settings: settings,
		svc:      phase1,
		phase1:   phase1,
		cancel:   cancel,
		lastUsed: time.Now(),
		refs:     0,
	}, nil
}

func (r *Registry) borrowHandleLocked(h *workspaceHandle, workspaceID string) (service.Service, func(), error) {
	h.lastUsed = time.Now()
	h.refs++
	svc := h.svc
	r.mu.Unlock()
	return svc, r.releaseFunc(workspaceID, h), nil
}

func (r *Registry) releaseFunc(workspaceID string, h *workspaceHandle) func() {
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		cur, ok := r.open[workspaceID]
		if !ok || cur != h {
			return
		}
		if h.refs <= 0 {
			slog.Warn("registry: double release of workspace borrow", "workspace_id", workspaceID)
			return
		}
		h.refs--
		if h.refs == 0 {
			h.lastUsed = time.Now()
			if r.closing {
				delete(r.open, workspaceID)
				r.discards++
				go r.discardHandle(h)
				return
			}
		}
	}
}

func (r *Registry) evictOldestIdleLocked() *workspaceHandle {
	var oldestID string
	var oldestTime time.Time
	first := true
	for id, h := range r.open {
		if h.refs > 0 {
			continue
		}
		if first || h.lastUsed.Before(oldestTime) {
			oldestID = id
			oldestTime = h.lastUsed
			first = false
		}
	}
	if oldestID == "" {
		return nil
	}
	h := r.open[oldestID]
	delete(r.open, oldestID)
	slog.Info("workspace evicted", "workspace_id", oldestID)
	return h
}

func (r *Registry) beginDiscard() {
	r.mu.Lock()
	r.discards++
	r.mu.Unlock()
}

func (r *Registry) discardHandle(h *workspaceHandle) {
	if h == nil {
		return
	}
	defer func() {
		r.mu.Lock()
		r.discards--
		r.mu.Unlock()
		r.tryShutdownRuntimeAfterDrain()
	}()
	if h.cancel != nil {
		h.cancel()
	}
	if h.phase1 != nil {
		if err := h.phase1.Close(); err != nil {
			slog.Warn("workspace discard", "err", err)
		}
	}
	if h.settings != nil {
		if profile, err := h.settings.ActiveProfile(); err == nil {
			zvec.ReclaimCollectionLock(zvec.Config{
				IndexDir:      h.settings.IndexDir,
				WorkspaceRoot: h.settings.WorkspaceRoot,
				ProfileName:   h.settings.App.ActiveProfile,
				Dimensions:    profile.Dimensions,
			})
		}
	}
}

// tryShutdownRuntimeAfterDrain runs global runtime teardown once the registry is
// closing and every open/opening/borrow/discard has drained. Safe if Close already
// shut down (runtimeShutdownDone + idempotent CloseResources/ShutdownRuntime).
func (r *Registry) tryShutdownRuntimeAfterDrain() {
	r.mu.Lock()
	if !r.closing || r.runtimeShutdownDone ||
		len(r.open) != 0 || len(r.opening) != 0 ||
		r.discards != 0 || r.hasBusyHandlesLocked() {
		r.mu.Unlock()
		return
	}
	r.runtimeShutdownDone = true
	hook := r.onRuntimeShutdown
	r.mu.Unlock()

	chunk.CloseResources()
	zvec.ShutdownRuntime()
	if hook != nil {
		hook()
	}
}

// Close shuts down all open workspace handles after in-flight borrows and cold-opens drain.
func (r *Registry) Close() {
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return
	}
	r.closing = true
	r.mu.Unlock()
	if r.closeCancel != nil {
		r.closeCancel()
	}

	drainTimeout := r.closeDrainTimeout
	if drainTimeout <= 0 {
		drainTimeout = defaultCloseDrainTimeout
	}
	deadline := time.Now().Add(drainTimeout)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	var doneCh <-chan struct{}
	if r.rootCtx != nil {
		doneCh = r.rootCtx.Done()
	}
	rootCanceled := false
	for r.hasBusyHandles() || r.hasOpening() || r.hasDiscards() {
		if time.Now().After(deadline) {
			slog.Warn("registry close: timeout waiting for borrows and cold-open to drain")
			break
		}
		select {
		case <-doneCh:
			if doneCh != nil {
				if !rootCanceled {
					slog.Warn("registry close: root context canceled while borrows remain, waiting for drain")
					rootCanceled = true
				}
				doneCh = nil
			}
		case <-tick.C:
		}
	}

	r.mu.Lock()
	type closeItem struct {
		id string
		h  *workspaceHandle
	}
	var toClose []closeItem
	for id, h := range r.open {
		if h != nil && h.refs > 0 {
			slog.Warn("registry close: skipping workspace with in-flight borrows", "workspace_id", id, "refs", h.refs)
			continue
		}
		toClose = append(toClose, closeItem{id: id, h: h})
		delete(r.open, id)
	}
	skipRuntime := r.hasBusyHandlesLocked() || len(r.opening) > 0 || r.discards > 0
	r.mu.Unlock()

	for _, item := range toClose {
		if item.h.cancel != nil {
			item.h.cancel()
		}
		if item.h.phase1 != nil {
			if err := item.h.phase1.Close(); err != nil {
				slog.Warn("workspace close", "workspace_id", item.id, "err", err)
			}
		}
		slog.Info("workspace evicted", "workspace_id", item.id)
	}
	if skipRuntime {
		slog.Warn("registry close: skipping zvec runtime shutdown while borrows or cold-open remain")
		return
	}
	r.tryShutdownRuntimeAfterDrain()
}

func (r *Registry) hasBusyHandles() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hasBusyHandlesLocked()
}

func (r *Registry) hasBusyHandlesLocked() bool {
	for _, h := range r.open {
		if h.refs > 0 {
			return true
		}
	}
	return false
}

func (r *Registry) hasOpening() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.opening) > 0
}

func (r *Registry) hasDiscards() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.discards > 0
}

// Config returns the daemon configuration.
func (r *Registry) Config() Config {
	return r.cfg
}
