package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/testutil"
)

// reloadFixture wires a user-level install with a global config, a projects.d
// directory and a project root per collection.
type reloadFixture struct {
	t      *testing.T
	user   string
	cfg    Config
	mu     sync.Mutex
	roots  map[string]string
	loads  int
	failOn error
}

func newReloadFixture(t *testing.T) *reloadFixture {
	t.Helper()
	user := t.TempDir()
	// Registry writes leave files in projects.d; on Windows the parent removal can
	// briefly fail with ERROR_DIR_NOT_EMPTY (pending-delete bookkeeping).
	t.Cleanup(func() { testutil.RemoveDirWithRetry(t, user) })
	t.Setenv("MCP_GLOBAL_DIR", user)
	if err := os.WriteFile(filepath.Join(user, "config.yaml"), []byte(testGlobalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return &reloadFixture{t: t, user: user, roots: map[string]string{}}
}

// addProject registers a collection on disk and returns its root.
func (f *reloadFixture) addProject(id, description string) string {
	f.t.Helper()
	root := f.t.TempDir()
	writeProjectYAML(f.t, root, id, description)
	writeRegistrationFile(f.t, f.user, id, root)
	f.mu.Lock()
	f.roots[id] = root
	f.mu.Unlock()
	return root
}

func (f *reloadFixture) root(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.roots[id]
}

// loader reloads the daemon config from disk, counting invocations and
// injecting failures for the atomic-failure test.
func (f *reloadFixture) loader() (Config, error) {
	f.mu.Lock()
	f.loads++
	failOn := f.failOn
	f.mu.Unlock()
	if failOn != nil {
		return Config{}, failOn
	}
	// Reload is strict in production (ConfigLoader uses LoadConfigStrict): a
	// partially valid snapshot must never be applied.
	cfg, err := LoadConfigStrict("")
	if err != nil {
		return Config{}, err
	}
	cfg.AutoIndexOnOpen = boolPointer(false)
	return cfg, nil
}

func boolPointer(v bool) *bool { return &v }

// newRegistry builds a registry over the fixture's current on-disk snapshot.
func (f *reloadFixture) newRegistry(ctx context.Context) *Registry {
	f.t.Helper()
	cfg, err := f.loader()
	if err != nil {
		f.t.Fatal(err)
	}
	r := NewRegistry(cfg, ctx)
	r.SetConfigLoader(f.loader)
	return r
}

// TestEmptyGlobalRegistry pins that a daemon with no projects is valid and an
// unknown collection is reported as unknown, never as the first collection.
func TestEmptyGlobalRegistry(t *testing.T) {
	f := newReloadFixture(t)
	r := f.newRegistry(t.Context())
	defer r.Close()

	if list := r.ListWorkspaces(false); len(list) != 0 {
		t.Fatalf("list=%+v", list)
	}
	if _, err := r.GetService("anything"); !errors.Is(err, ErrUnknownWorkspace) {
		t.Fatalf("err=%v want ErrUnknownWorkspace", err)
	}
	// Reload of an empty registry is a no-op, not an error.
	if err := r.ReloadProjects(t.Context()); err != nil {
		t.Fatalf("reload of empty registry: %v", err)
	}
}

// TestDaemonLoadsProjectOverrides proves a projects.d collection is opened with
// its project.yaml overrides applied, its id matching the registration, and its
// index under the user-level collections dir.
func TestDaemonLoadsProjectOverrides(t *testing.T) {
	f := newReloadFixture(t)
	root := f.addProject("col-a", "A")
	// Override that must be visible in the opened settings.
	cfgPath := config.ProjectConfigPath(root)
	body := "schema_version: 1\ncollection_id: col-a\nindexing:\n  extensions: [\".bsl\"]\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	r := f.newRegistry(t.Context())
	defer r.Close()

	svc, release, err := r.BorrowService("col-a")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	raw, err := svc.GetIndexStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("empty status payload")
	}

	r.mu.Lock()
	h := r.open["col-a"]
	r.mu.Unlock()
	if h == nil {
		t.Fatal("handle not opened")
	}
	if h.settings.CollectionID != "col-a" || h.settings.WorkspaceID != "col-a" {
		t.Fatalf("settings ids: collection=%q workspace=%q", h.settings.CollectionID, h.settings.WorkspaceID)
	}
	if h.settings.WorkspaceRoot != config.NormalizeAbsolutePath(root) {
		t.Fatalf("workspace_root=%q want %q", h.settings.WorkspaceRoot, config.NormalizeAbsolutePath(root))
	}
	if got := h.settings.App.Indexing.Extensions; len(got) != 1 || got[0] != ".bsl" {
		t.Fatalf("project overrides not applied: %v", got)
	}
	wantIndex, err := config.CollectionDir("col-a")
	if err != nil {
		t.Fatal(err)
	}
	if h.settings.IndexDir != wantIndex {
		t.Fatalf("index_dir=%q want %q", h.settings.IndexDir, wantIndex)
	}
}

// TestReloadAddsWithoutClosingExisting: a new project becomes available after
// reload while the existing collection keeps its handle.
func TestReloadAddsWithoutClosingExisting(t *testing.T) {
	f := newReloadFixture(t)
	f.addProject("col-a", "A")
	r := f.newRegistry(t.Context())
	defer r.Close()

	if _, err := r.GetService("col-a"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	handleA := r.open["col-a"]
	r.mu.Unlock()
	if handleA == nil {
		t.Fatal("A was not opened")
	}

	f.addProject("col-b", "B")
	if err := r.ReloadProjects(t.Context()); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if _, err := r.GetService("col-b"); err != nil {
		t.Fatalf("B must be available after reload: %v", err)
	}
	r.mu.Lock()
	sameHandle := r.open["col-a"] == handleA
	openB := r.open["col-b"] != nil
	r.mu.Unlock()
	if !sameHandle {
		t.Fatal("A must keep the same open handle across reload")
	}
	if !openB {
		t.Fatal("B must be open after its first use")
	}
	if list := r.ListWorkspaces(false); len(list) != 2 {
		t.Fatalf("list=%+v", list)
	}
}

// TestReloadAtomicFailure: one invalid registration rejects the whole snapshot
// and the running collection keeps being served.
func TestReloadAtomicFailure(t *testing.T) {
	f := newReloadFixture(t)
	f.addProject("col-a", "A")
	r := f.newRegistry(t.Context())
	defer r.Close()
	if _, err := r.GetService("col-a"); err != nil {
		t.Fatal(err)
	}

	// A malformed registration appears on disk: reload must fail and change nothing.
	badPath := filepath.Join(f.user, "projects.d", "broken.yaml")
	if err := os.WriteFile(badPath, []byte("id: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := r.ReloadProjects(t.Context())
	if err == nil {
		t.Fatal("reload with an invalid snapshot must fail")
	}
	var reloadErr *ReloadError
	if !errors.As(err, &reloadErr) || reloadErr.Kind != ReloadInvalidSnapshot {
		t.Fatalf("err=%v want ReloadInvalidSnapshot", err)
	}
	if _, err := r.GetService("col-a"); err != nil {
		t.Fatalf("A must keep serving after a failed reload: %v", err)
	}
	if list := r.ListWorkspaces(false); len(list) != 1 || list[0].ID != "col-a" {
		t.Fatalf("snapshot changed by a failed reload: %+v", list)
	}

	// A loader error is also an invalid snapshot.
	if err := os.Remove(badPath); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.failOn = errors.New("disk on fire")
	f.mu.Unlock()
	if err := r.ReloadProjects(t.Context()); err == nil {
		t.Fatal("loader failure must fail the reload")
	}
	if _, err := r.GetService("col-a"); err != nil {
		t.Fatalf("A must keep serving after a loader failure: %v", err)
	}
}

// TestReloadBusyChange: changing or removing a collection that is in use is
// refused with a busy error and nothing is applied.
func TestReloadBusyChange(t *testing.T) {
	f := newReloadFixture(t)
	rootA := f.addProject("col-a", "A")
	r := f.newRegistry(t.Context())
	defer r.Close()

	_, releaseA, err := r.BorrowService("col-a")
	if err != nil {
		t.Fatal(err)
	}

	// Change A's project overrides and add B: A is busy, so nothing applies.
	cfgPath := config.ProjectConfigPath(rootA)
	if err := os.WriteFile(cfgPath, []byte("schema_version: 1\ncollection_id: col-a\nindexing:\n  extensions: [\".bsl\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.addProject("col-b", "B")

	err = r.ReloadProjects(t.Context())
	var reloadErr *ReloadError
	if !errors.As(err, &reloadErr) || reloadErr.Kind != ReloadBusy {
		t.Fatalf("err=%v want ReloadBusy", err)
	}
	if len(reloadErr.IDs) != 1 || reloadErr.IDs[0] != "col-a" {
		t.Fatalf("busy ids=%v", reloadErr.IDs)
	}
	if _, err := r.GetService("col-b"); !errors.Is(err, ErrUnknownWorkspace) {
		t.Fatalf("B must not be added partially: %v", err)
	}

	// After release the same snapshot applies fully.
	releaseA()
	if err := r.ReloadProjects(t.Context()); err != nil {
		t.Fatalf("reload after release: %v", err)
	}
	if _, err := r.GetService("col-b"); err != nil {
		t.Fatalf("B must be available after the retry: %v", err)
	}
	// A was detached by the reload and reopens lazily with the new overrides.
	if _, err := r.GetService("col-a"); err != nil {
		t.Fatalf("A must reopen after the reload: %v", err)
	}
	r.mu.Lock()
	h := r.open["col-a"]
	r.mu.Unlock()
	if h == nil {
		t.Fatal("A was not reopened after the reload")
	}
	if got := h.settings.App.Indexing.Extensions; len(got) != 1 || got[0] != ".bsl" {
		t.Fatalf("A overrides not reloaded: %v", got)
	}
}

// TestReloadBusyRemovedCollection: removing a collection that is in use is also
// refused, so its data cannot be detached under an in-flight request.
func TestReloadBusyRemovedCollection(t *testing.T) {
	f := newReloadFixture(t)
	f.addProject("col-a", "A")
	f.addProject("col-b", "B")
	r := f.newRegistry(t.Context())
	defer r.Close()

	_, releaseB, err := r.BorrowService("col-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.user, "projects.d", "col-b.yaml")); err != nil {
		t.Fatal(err)
	}

	err = r.ReloadProjects(t.Context())
	var reloadErr *ReloadError
	if !errors.As(err, &reloadErr) || reloadErr.Kind != ReloadBusy {
		t.Fatalf("err=%v want ReloadBusy", err)
	}
	if list := r.ListWorkspaces(false); len(list) != 2 {
		t.Fatalf("snapshot must be unchanged: %+v", list)
	}
	releaseB()

	if err := r.ReloadProjects(t.Context()); err != nil {
		t.Fatalf("reload after release: %v", err)
	}
	if list := r.ListWorkspaces(false); len(list) != 1 || list[0].ID != "col-a" {
		t.Fatalf("list=%+v", list)
	}
}

// TestReloadReservesChangedCollectionWhileClosing pins the ordering that keeps a
// reload from racing a concurrent request: while the old handle of a changed
// collection is closing, a borrower must wait instead of opening the same index
// dir (which would collide with the old handle's zvec LOCK).
func TestReloadReservesChangedCollectionWhileClosing(t *testing.T) {
	f := newReloadFixture(t)
	root := f.addProject("col-a", "A")
	r := f.newRegistry(t.Context())
	defer r.Close()

	if _, err := r.GetService("col-a"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProjectConfigPath(root),
		[]byte("schema_version: 1\ncollection_id: col-a\nindexing:\n  skip_dirs: [vendor]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A borrower racing the reload must not observe a half-applied snapshot.
	borrowStarted := make(chan struct{})
	borrowDone := make(chan error, 1)
	go func() {
		close(borrowStarted)
		_, release, err := r.BorrowService("col-a")
		if release != nil {
			release()
		}
		borrowDone <- err
	}()
	<-borrowStarted

	if err := r.ReloadProjects(t.Context()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := <-borrowDone; err != nil {
		t.Fatalf("a concurrent borrow must survive a reload: %v", err)
	}

	// No reservation and at most one handle survive the reload.
	r.mu.Lock()
	openCount := len(r.open)
	openingCount := len(r.opening)
	r.mu.Unlock()
	if openingCount != 0 {
		t.Fatalf("reload left %d reservation(s) behind", openingCount)
	}
	if openCount > 1 {
		t.Fatalf("reload left %d handles for one collection", openCount)
	}

	if _, err := r.GetService("col-a"); err != nil {
		t.Fatalf("collection must be usable after the reload: %v", err)
	}
	r.mu.Lock()
	h := r.open["col-a"]
	r.mu.Unlock()
	if h == nil {
		t.Fatal("collection not reopened")
	}
	if got := h.settings.App.Indexing.SkipDirs; len(got) != 1 || got[0] != "vendor" {
		t.Fatalf("new overrides not applied: %v", got)
	}
}

// TestReloadClosesIdleChangedHandle: an idle handle whose project config changed
// is closed and reopened with the new settings.
func TestReloadClosesIdleChangedHandle(t *testing.T) {
	f := newReloadFixture(t)
	root := f.addProject("col-a", "A")
	r := f.newRegistry(t.Context())
	defer r.Close()

	if _, err := r.GetService("col-a"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	before := r.open["col-a"]
	r.mu.Unlock()

	if err := os.WriteFile(config.ProjectConfigPath(root),
		[]byte("schema_version: 1\ncollection_id: col-a\nindexing:\n  skip_dirs: [vendor]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.ReloadProjects(t.Context()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	r.mu.Lock()
	_, stillOpen := r.open["col-a"]
	r.mu.Unlock()
	if stillOpen {
		t.Fatal("an idle changed collection must be closed by reload")
	}

	if _, err := r.GetService("col-a"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	after := r.open["col-a"]
	r.mu.Unlock()
	if after == before {
		t.Fatal("the collection must be reopened with a fresh handle")
	}
	if got := after.settings.App.Indexing.SkipDirs; len(got) != 1 || got[0] != "vendor" {
		t.Fatalf("new overrides not applied: %v", got)
	}
}

// TestReloadNotConfigured: a registry without a loader reports a clear error
// instead of silently doing nothing.
func TestReloadNotConfigured(t *testing.T) {
	r := NewRegistry(Config{MaxOpenWorkspaces: 1}, t.Context())
	defer r.Close()
	if err := r.ReloadProjects(t.Context()); !errors.Is(err, ErrReloadNotConfigured) {
		t.Fatalf("err=%v", err)
	}
}

// TestReloadAfterClose: reload during shutdown reports the closing error.
func TestReloadAfterClose(t *testing.T) {
	f := newReloadFixture(t)
	r := f.newRegistry(t.Context())
	r.Close()
	if err := r.ReloadProjects(t.Context()); !errors.Is(err, ErrRegistryClosing) {
		t.Fatalf("err=%v", err)
	}
}

// TestCollectionIdle covers the guard used before a project relocation.
func TestCollectionIdle(t *testing.T) {
	f := newReloadFixture(t)
	f.addProject("col-a", "A")
	r := f.newRegistry(t.Context())
	defer r.Close()

	// Not open: idle.
	if err := r.CollectionIdle("col-a"); err != nil {
		t.Fatalf("unopened collection must be idle: %v", err)
	}
	_, release, err := r.BorrowService("col-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CollectionIdle("col-a"); !errors.Is(err, ErrCollectionBusy) {
		t.Fatalf("err=%v want ErrCollectionBusy", err)
	}
	release()
	// The release callback updates lastUsed; idle again once refs drop to zero.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := r.CollectionIdle("col-a"); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("collection did not become idle after release")
}

// TestReloadConcurrentSerialized: concurrent reloads do not interleave, and the
// final snapshot is consistent.
func TestReloadConcurrentSerialized(t *testing.T) {
	f := newReloadFixture(t)
	f.addProject("col-a", "A")
	r := f.newRegistry(t.Context())
	defer r.Close()

	f.addProject("col-b", "B")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- r.ReloadProjects(t.Context())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent reload: %v", err)
		}
	}
	if list := r.ListWorkspaces(false); len(list) != 2 {
		t.Fatalf("list=%+v", list)
	}
}
