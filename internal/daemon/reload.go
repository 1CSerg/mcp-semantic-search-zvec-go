package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ErrReloadNotConfigured is returned when the registry has no config loader
// attached, so it cannot rebuild its project snapshot.
var ErrReloadNotConfigured = errors.New("project reload is not configured")

// ErrCollectionBusy is returned when a collection cannot be reloaded or
// relocated because it still has open handles or a running indexing job.
var ErrCollectionBusy = errors.New("collection is busy")

// ReloadErrorKind classifies a reload failure so the HTTP layer can map it to a
// status code without string matching.
type ReloadErrorKind int

const (
	// ReloadInvalidSnapshot means the new projects.d snapshot could not be read
	// or validated; the previous snapshot stays in effect.
	ReloadInvalidSnapshot ReloadErrorKind = iota + 1
	// ReloadBusy means a changed or removed collection is still in use; nothing
	// was applied.
	ReloadBusy
)

// ReloadError is the typed error returned by Registry.ReloadProjects.
type ReloadError struct {
	Kind ReloadErrorKind
	IDs  []string
	Err  error
}

func (e *ReloadError) Error() string {
	switch e.Kind {
	case ReloadBusy:
		return fmt.Sprintf("%v: %s", ErrCollectionBusy, strings.Join(e.IDs, ", "))
	default:
		if e.Err != nil {
			return fmt.Sprintf("invalid project snapshot: %v", e.Err)
		}
		return "invalid project snapshot"
	}
}

func (e *ReloadError) Unwrap() error {
	if e.Kind == ReloadBusy {
		return ErrCollectionBusy
	}
	return e.Err
}

// SetConfigLoader installs the loader used by ReloadProjects to rebuild the
// daemon config from the same source it started with.
func (r *Registry) SetConfigLoader(load func() (Config, error)) {
	r.mu.Lock()
	r.reload = load
	r.mu.Unlock()
}

// CollectionIdle reports an error when the collection currently has open
// handles with in-flight requests or a running indexing job. Used to guard a
// relocation: the root must not change under an active service.
func (r *Registry) CollectionIdle(collectionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return ErrRegistryClosing
	}
	if _, opening := r.opening[collectionID]; opening {
		return fmt.Errorf("%w: collection is opening", ErrCollectionBusy)
	}
	h, ok := r.open[collectionID]
	if !ok {
		return nil
	}
	if h.refs > 0 {
		return fmt.Errorf("%w: %d in-flight request(s)", ErrCollectionBusy, h.refs)
	}
	if h.phase1 != nil && h.phase1.IndexingActive() {
		return fmt.Errorf("%w: indexing is running", ErrCollectionBusy)
	}
	return nil
}

// specsEqual reports whether two specs describe the same collection identity.
// Description is excluded on purpose: it is metadata, not index identity.
func specsEqual(a, b WorkspaceSpec) bool {
	return a.ID == b.ID &&
		a.Root == b.Root &&
		a.IndexDir == b.IndexDir &&
		a.ConfigPath == b.ConfigPath &&
		a.ProjectDigest == b.ProjectDigest &&
		a.FromProjectsDir == b.FromProjectsDir
}

// ReloadProjects re-reads the project registry and applies the new snapshot.
//
// The snapshot is read and validated in full before anything is applied, so a
// broken registration leaves the running daemon untouched. Unchanged collections
// keep their handles, watchers and indexing jobs; new ones are opened lazily on
// first use; changed or removed collections are closed only when idle, otherwise
// the whole reload is refused with ReloadBusy.
func (r *Registry) ReloadProjects(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Serialize reloads so two concurrent reloads cannot interleave snapshots.
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()

	r.mu.Lock()
	loader := r.reload
	closing := r.closing
	r.mu.Unlock()
	if closing {
		return ErrRegistryClosing
	}
	if loader == nil {
		return ErrReloadNotConfigured
	}

	newCfg, err := loader()
	if err != nil {
		return &ReloadError{Kind: ReloadInvalidSnapshot, Err: err}
	}

	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return ErrRegistryClosing
	}
	oldCfg := r.cfg

	var changed []string
	for _, oldSpec := range oldCfg.Workspaces {
		newSpec := newCfg.SpecByID(oldSpec.ID)
		if newSpec == nil || !specsEqual(oldSpec, *newSpec) {
			changed = append(changed, oldSpec.ID)
		}
	}

	// Refuse the whole snapshot if any changed/removed collection is in use.
	var busy []string
	for _, id := range changed {
		if _, opening := r.opening[id]; opening {
			busy = append(busy, id)
			continue
		}
		if h, ok := r.open[id]; ok {
			if h.refs > 0 || (h.phase1 != nil && h.phase1.IndexingActive()) {
				busy = append(busy, id)
			}
		}
	}
	if len(busy) > 0 {
		r.mu.Unlock()
		slog.Warn("project reload refused: collections in use", "collections", busy)
		return &ReloadError{Kind: ReloadBusy, IDs: busy}
	}

	// Apply: swap the snapshot and detach the affected idle handles. Handles are
	// closed outside the registry mutex so a slow drain cannot block borrowers.
	//
	// Each changed collection keeps a reservation in r.opening while its old
	// handle closes. Without it a request arriving in that window would find no
	// open handle, start a new open of the same index dir, and collide with the
	// old handle's zvec LOCK.
	var toClose []*workspaceHandle
	reservations := make(map[string]*openWait, len(changed))
	for _, id := range changed {
		if h, ok := r.open[id]; ok {
			delete(r.open, id)
			toClose = append(toClose, h)
		}
		wait := &openWait{done: make(chan struct{})}
		r.opening[id] = wait
		reservations[id] = wait
	}
	r.cfg = newCfg
	r.mu.Unlock()

	for _, h := range toClose {
		start := time.Now()
		r.beginDiscard()
		r.discardHandle(h)
		slog.Info("workspace closed by reload", "workspace_id", h.spec.ID, "elapsed", time.Since(start).String())
	}

	// Release the reservations so the next request opens the collection fresh.
	r.mu.Lock()
	for id, wait := range reservations {
		if r.opening[id] == wait {
			delete(r.opening, id)
		}
		close(wait.done)
	}
	r.mu.Unlock()

	slog.Info("project registry reloaded", "collections", len(newCfg.Workspaces), "closed", len(toClose))
	return nil
}
