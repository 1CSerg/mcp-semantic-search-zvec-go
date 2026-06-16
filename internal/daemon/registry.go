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
)

// ErrUnknownWorkspace is returned when workspace_id is not registered.
var ErrUnknownWorkspace = errors.New("unknown workspace")

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
}

// Registry manages lazy-open workspace services with LRU eviction.
type Registry struct {
	cfg     Config
	rootCtx context.Context

	mu   sync.Mutex
	open map[string]*workspaceHandle
}

// NewRegistry creates a workspace registry from daemon config.
func NewRegistry(cfg Config, rootCtx context.Context) *Registry {
	if rootCtx == nil {
		rootCtx = context.Background()
	}
	return &Registry{
		cfg:     cfg,
		rootCtx: rootCtx,
		open:    make(map[string]*workspaceHandle),
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

// GetService returns the service for workspace_id, opening it if needed.
func (r *Registry) GetService(workspaceID string) (service.Service, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required")
	}
	spec := r.cfg.SpecByID(workspaceID)
	if spec == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownWorkspace, workspaceID)
	}

	r.mu.Lock()
	if h, ok := r.open[workspaceID]; ok {
		h.lastUsed = time.Now()
		svc := h.svc
		r.mu.Unlock()
		return svc, nil
	}
	r.mu.Unlock()

	return r.openWorkspace(*spec)
}

func (r *Registry) openWorkspace(spec WorkspaceSpec) (service.Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if h, ok := r.open[spec.ID]; ok {
		h.lastUsed = time.Now()
		return h.svc, nil
	}

	if len(r.open) >= r.cfg.MaxOpenWorkspaces {
		r.evictOldestLocked()
	}

	settings, err := config.LoadWorkspaceFromSpec(spec.ID, spec.Root, spec.IndexDir, spec.ConfigPath, config.LoadOptions{
		PathContainment: config.ParsePathContainmentMode(r.cfg.PathContainment),
		PathAllowlist:   r.cfg.PathAllowlist,
	})
	if err != nil {
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

	h := &workspaceHandle{
		spec:     spec,
		settings: settings,
		svc:      phase1,
		phase1:   phase1,
		cancel:   cancel,
		lastUsed: time.Now(),
	}
	r.open[spec.ID] = h
	slog.Info("workspace opened", "workspace_id", spec.ID, "root", spec.Root)
	return phase1, nil
}

func (r *Registry) evictOldestLocked() {
	var oldestID string
	var oldestTime time.Time
	first := true
	for id, h := range r.open {
		if first || h.lastUsed.Before(oldestTime) {
			oldestID = id
			oldestTime = h.lastUsed
			first = false
		}
	}
	if oldestID != "" {
		r.closeHandleLocked(oldestID)
	}
}

func (r *Registry) closeHandleLocked(id string) {
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

// Close shuts down all open workspace handles.
func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id := range r.open {
		r.closeHandleLocked(id)
	}
}

// Config returns the daemon configuration.
func (r *Registry) Config() Config {
	return r.cfg
}
