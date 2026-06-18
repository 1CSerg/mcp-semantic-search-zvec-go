package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
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

	mu                sync.Mutex
	open              map[string]*workspaceHandle
	opening           map[string]*openWait
	closing           bool
	closeDrainTimeout time.Duration
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

		if err := r.reserveOpenSlotLocked(*spec); err != nil {
			r.mu.Unlock()
			return nil, nil, err
		}
		wait := &openWait{done: make(chan struct{})}
		r.opening[workspaceID] = wait
		r.mu.Unlock()

		h, err := r.initWorkspace(*spec)
		r.mu.Lock()
		delete(r.opening, workspaceID)
		if r.closing {
			wait.err = ErrRegistryClosing
			close(wait.done)
			if err == nil && h != nil {
				r.mu.Unlock()
				r.discardHandle(h)
			} else {
				if err != nil {
					wait.err = err
				}
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

func (r *Registry) reserveOpenSlotLocked(spec WorkspaceSpec) error {
	if _, ok := r.open[spec.ID]; ok {
		return nil
	}
	if _, ok := r.opening[spec.ID]; ok {
		return nil
	}
	occupied := len(r.open) + len(r.opening)
	if occupied >= r.cfg.MaxOpenWorkspaces {
		if !r.evictOldestIdleLocked() {
			return fmt.Errorf("max open workspaces (%d) reached and all are in use", r.cfg.MaxOpenWorkspaces)
		}
	}
	return nil
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

	phase1, err := service.NewPhase1(settings)
	if err != nil {
		return nil, err
	}
	wsCtx, cancel := context.WithCancel(r.rootCtx)
	phase1.SetLifecycleContext(wsCtx)
	phase1.StartFileWatcher(wsCtx)
	phase1.PrepareStartup()

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
	return svc, r.releaseFunc(workspaceID), nil
}

func (r *Registry) releaseFunc(workspaceID string) func() {
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		h, ok := r.open[workspaceID]
		if !ok {
			return
		}
		if h.refs <= 0 {
			slog.Warn("registry: double release of workspace borrow", "workspace_id", workspaceID)
			return
		}
		h.refs--
	}
}

func (r *Registry) evictOldestIdleLocked() bool {
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
		return false
	}
	r.forceCloseHandleLocked(oldestID)
	return true
}

func (r *Registry) closeHandleLocked(id string) {
	h, ok := r.open[id]
	if !ok {
		return
	}
	if h.refs > 0 {
		return
	}
	r.forceCloseHandleLocked(id)
}

func (r *Registry) discardHandle(h *workspaceHandle) {
	if h == nil {
		return
	}
	if h.cancel != nil {
		h.cancel()
	}
	if h.phase1 != nil {
		if err := h.phase1.Close(); err != nil {
			slog.Warn("workspace discard", "err", err)
		}
	}
}

func (r *Registry) forceCloseHandleLocked(id string) {
	h, ok := r.open[id]
	if !ok {
		return
	}
	if h.cancel != nil {
		h.cancel()
	}
	if h.phase1 != nil {
		if err := h.phase1.Close(); err != nil {
			slog.Warn("workspace close", "workspace_id", id, "err", err)
		}
	}
	delete(r.open, id)
	slog.Info("workspace evicted", "workspace_id", id)
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
	rootCanceled := false
	for r.hasBusyHandles() || r.hasOpening() {
		if time.Now().After(deadline) {
			slog.Warn("registry close: timeout waiting for borrows and cold-open to drain")
			break
		}
		select {
		case <-r.rootCtx.Done():
			if !rootCanceled {
				slog.Warn("registry close: root context canceled while borrows remain, waiting for drain")
				rootCanceled = true
			}
		case <-tick.C:
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for id, h := range r.open {
		if h != nil && h.refs > 0 {
			slog.Warn("registry close: skipping workspace with in-flight borrows", "workspace_id", id, "refs", h.refs)
			continue
		}
		r.forceCloseHandleLocked(id)
	}
	if r.hasBusyHandlesLocked() || len(r.opening) > 0 {
		slog.Warn("registry close: skipping zvec runtime shutdown while borrows or cold-open remain")
		return
	}
	zvec.ShutdownRuntime()
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

// Config returns the daemon configuration.
func (r *Registry) Config() Config {
	return r.cfg
}
