package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemonclient"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/testutil"
)

// stubDaemon records what the management commands asked the daemon to do, so no
// real daemon process is ever spawned by these tests.
type stubDaemon struct {
	ensureErr   error
	reloadErr   error
	running     bool
	runningErr  error
	stopErr     error
	collection  bool
	collectionE error

	ensureCalls int
	reloadCalls int
	stopCalls   int
}

func (s *stubDaemon) EnsureRunning(context.Context) (daemonclient.Connection, error) {
	s.ensureCalls++
	if s.ensureErr != nil {
		return daemonclient.Connection{}, s.ensureErr
	}
	return daemonclient.Connection{BaseURL: "http://127.0.0.1:8080", Token: "t"}, nil
}

func (s *stubDaemon) Stop(context.Context) error {
	s.stopCalls++
	return s.stopErr
}

func (s *stubDaemon) Running(context.Context) (bool, error) { return s.running, s.runningErr }

func (s *stubDaemon) ReloadProjects(context.Context) error {
	s.reloadCalls++
	return s.reloadErr
}

func (s *stubDaemon) CollectionOpen(context.Context, string) (bool, error) {
	return s.collection, s.collectionE
}

// withStubDaemon installs a stub daemon client for the duration of a test.
func withStubDaemon(t *testing.T, stub *stubDaemon) *stubDaemon {
	t.Helper()
	prev := newDaemonClient
	newDaemonClient = func() daemonAPI { return stub }
	t.Cleanup(func() { newDaemonClient = prev })
	return stub
}

// commandEnv prepares a user-level install and returns its dir.
func commandEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Registry writes leave files in projects.d; on Windows the parent removal can
	// briefly fail with ERROR_DIR_NOT_EMPTY (pending-delete bookkeeping).
	t.Cleanup(func() { testutil.RemoveDirWithRetry(t, dir) })
	t.Setenv("MCP_GLOBAL_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(projectProxyGlobalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func registrations(t *testing.T) []daemon.Registration {
	t.Helper()
	dir, err := config.UserDataDir()
	if err != nil {
		t.Fatal(err)
	}
	regs, err := daemon.NewProjectManager(dir).Registrations()
	if err != nil {
		t.Fatal(err)
	}
	return regs
}

// TestCommandProjectAddRegistersAndReloads: add registers the project, ensures
// the daemon and reloads it, and re-adding is idempotent.
func TestCommandProjectAddRegistersAndReloads(t *testing.T) {
	commandEnv(t)
	stub := withStubDaemon(t, &stubDaemon{})
	root := t.TempDir()

	if err := commandProjectAdd(t.Context(), root); err != nil {
		t.Fatalf("add: %v", err)
	}
	if stub.ensureCalls != 1 || stub.reloadCalls != 1 {
		t.Fatalf("ensure=%d reload=%d", stub.ensureCalls, stub.reloadCalls)
	}
	regs := registrations(t)
	if len(regs) != 1 {
		t.Fatalf("registrations=%v", regs)
	}
	id := regs[0].ID

	if err := commandProjectAdd(t.Context(), root); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if regs := registrations(t); len(regs) != 1 || regs[0].ID != id {
		t.Fatalf("registrations=%v", regs)
	}
}

// TestCommandProjectAddRollbackOnReloadConflict: when the daemon refuses the new
// snapshot because a collection is busy, the registration this command created is
// rolled back so disk state matches what the daemon serves.
func TestCommandProjectAddRollbackOnReloadConflict(t *testing.T) {
	commandEnv(t)
	withStubDaemon(t, &stubDaemon{
		reloadErr: daemonclient.ErrReloadConflict,
	})
	root := t.TempDir()

	err := commandProjectAdd(t.Context(), root)
	if err == nil {
		t.Fatal("a reload conflict must surface as an error")
	}
	if !errors.Is(err, daemonclient.ErrReloadConflict) {
		t.Fatalf("err=%v want ErrReloadConflict", err)
	}
	if regs := registrations(t); len(regs) != 0 {
		t.Fatalf("registration must be rolled back: %v", regs)
	}
	if _, statErr := os.Stat(config.ProjectConfigPath(root)); !os.IsNotExist(statErr) {
		t.Fatalf("project.yaml must be rolled back too: %v", statErr)
	}
}

// TestCommandProjectAddKeepsRegistrationWhenDaemonUnavailable: a daemon that
// cannot be started is not a reason to lose the registration; the next reload or
// restart picks it up.
func TestCommandProjectAddKeepsRegistrationWhenDaemonUnavailable(t *testing.T) {
	commandEnv(t)
	withStubDaemon(t, &stubDaemon{ensureErr: errors.New("no daemon")})
	root := t.TempDir()

	if err := commandProjectAdd(t.Context(), root); err == nil {
		t.Fatal("the daemon failure must be reported")
	}
	if regs := registrations(t); len(regs) != 1 {
		t.Fatalf("registration must be kept: %v", regs)
	}
}

// TestCommandProjectRemoveKeepsCollectionData: removal drops the registration and
// project.yaml but never the collection directory.
func TestCommandProjectRemoveKeepsCollectionData(t *testing.T) {
	commandEnv(t)
	stub := withStubDaemon(t, &stubDaemon{running: true})
	root := t.TempDir()
	if err := commandProjectAdd(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	regs := registrations(t)
	if len(regs) != 1 {
		t.Fatalf("registrations=%v", regs)
	}
	collectionDir, err := config.CollectionDir(regs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(collectionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(collectionDir, "keep")
	if err := os.WriteFile(marker, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloadBefore := stub.reloadCalls

	if err := commandProjectRemove(t.Context(), root); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if regs := registrations(t); len(regs) != 0 {
		t.Fatalf("registrations=%v", regs)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("collection data must survive removal: %v", err)
	}
	if _, err := os.Stat(config.ProjectConfigPath(root)); !os.IsNotExist(err) {
		t.Fatalf("managed project.yaml must be removed: %v", err)
	}
	if stub.reloadCalls != reloadBefore+1 {
		t.Fatalf("a running daemon must be reloaded after removal: %d -> %d", reloadBefore, stub.reloadCalls)
	}
}

// TestCommandProjectRemoveWithoutDaemon: removing a project when no daemon runs
// must not start one.
func TestCommandProjectRemoveWithoutDaemon(t *testing.T) {
	commandEnv(t)
	stub := withStubDaemon(t, &stubDaemon{running: false})
	root := t.TempDir()
	if err := commandProjectAdd(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	stub.ensureCalls = 0
	stub.reloadCalls = 0
	if err := commandProjectRemove(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if stub.ensureCalls != 0 {
		t.Fatalf("remove must not start a daemon: ensure=%d", stub.ensureCalls)
	}
	if stub.reloadCalls != 0 {
		t.Fatalf("remove must not reload a stopped daemon: reload=%d", stub.reloadCalls)
	}
}

// TestCommandProjectNewCollection: a copied project gets a new id and the
// original keeps its own registration.
func TestCommandProjectNewCollection(t *testing.T) {
	commandEnv(t)
	withStubDaemon(t, &stubDaemon{})
	original := t.TempDir()
	if err := commandProjectAdd(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	originalID := registrations(t)[0].ID

	copied := t.TempDir()
	src, err := os.ReadFile(config.ProjectConfigPath(original))
	if err != nil {
		t.Fatal(err)
	}
	dst := config.ProjectConfigPath(copied)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, src, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := commandProjectNewCollection(t.Context(), copied); err != nil {
		t.Fatalf("new collection: %v", err)
	}
	regs := registrations(t)
	if len(regs) != 2 {
		t.Fatalf("registrations=%v", regs)
	}
	var copiedID string
	for _, reg := range regs {
		if config.NormalizeAbsolutePath(reg.Root) == config.NormalizeAbsolutePath(copied) {
			copiedID = reg.ID
		}
	}
	if copiedID == "" || copiedID == originalID {
		t.Fatalf("copy id=%q original=%q", copiedID, originalID)
	}
	pc, err := config.LoadProjectConfig(config.ProjectConfigPath(original))
	if err != nil {
		t.Fatal(err)
	}
	if pc.CollectionID != originalID {
		t.Fatalf("original id changed: %q", pc.CollectionID)
	}
}

// TestCommandProjectRemoveRollsBackOnReloadConflict: when the daemon still serves
// the collection, the removal must not stand — both the registration and the
// managed project.yaml are restored, so disk state matches what is served.
func TestCommandProjectRemoveRollsBackOnReloadConflict(t *testing.T) {
	commandEnv(t)
	root := t.TempDir()
	withStubDaemon(t, &stubDaemon{})
	if err := commandProjectAdd(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	before := registrations(t)
	if len(before) != 1 {
		t.Fatalf("registrations=%v", before)
	}
	cfgBefore, err := os.ReadFile(config.ProjectConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}

	withStubDaemon(t, &stubDaemon{running: true, reloadErr: daemonclient.ErrReloadConflict})
	err = commandProjectRemove(t.Context(), root)
	if err == nil {
		t.Fatal("a reload conflict must surface as an error")
	}
	if !errors.Is(err, daemonclient.ErrReloadConflict) {
		t.Fatalf("err=%v want ErrReloadConflict", err)
	}

	// Everything is back: registration, project.yaml, same id.
	after := registrations(t)
	if len(after) != 1 || after[0].ID != before[0].ID {
		t.Fatalf("registration was not restored: %v", after)
	}
	cfgAfter, err := os.ReadFile(config.ProjectConfigPath(root))
	if err != nil {
		t.Fatalf("project.yaml was not restored: %v", err)
	}
	if string(cfgAfter) != string(cfgBefore) {
		t.Fatalf("project.yaml changed:\n got: %q\nwant: %q", cfgAfter, cfgBefore)
	}
}

// TestCommandProjectRelocateRollsBackOnReloadConflict: a refused snapshot leaves
// the registration pointing at the root the daemon actually serves.
func TestCommandProjectRelocateRollsBackOnReloadConflict(t *testing.T) {
	commandEnv(t)
	withStubDaemon(t, &stubDaemon{})
	oldRoot := t.TempDir()
	if err := commandProjectAdd(t.Context(), oldRoot); err != nil {
		t.Fatal(err)
	}
	id := registrations(t)[0].ID

	newRoot := t.TempDir()
	src, err := os.ReadFile(config.ProjectConfigPath(oldRoot))
	if err != nil {
		t.Fatal(err)
	}
	dst := config.ProjectConfigPath(newRoot)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, src, 0o600); err != nil {
		t.Fatal(err)
	}
	// A real move takes the old project config away.
	if err := os.Remove(config.ProjectConfigPath(oldRoot)); err != nil {
		t.Fatal(err)
	}

	withStubDaemon(t, &stubDaemon{running: true, reloadErr: daemonclient.ErrReloadConflict})
	err = commandProjectRelocate(t.Context(), newRoot, true)
	if err == nil {
		t.Fatal("a reload conflict must surface as an error")
	}
	if !errors.Is(err, daemonclient.ErrReloadConflict) {
		t.Fatalf("err=%v want ErrReloadConflict", err)
	}

	regs := registrations(t)
	if len(regs) != 1 || regs[0].ID != id {
		t.Fatalf("registrations=%v", regs)
	}
	if config.NormalizeAbsolutePath(regs[0].Root) != config.NormalizeAbsolutePath(oldRoot) {
		t.Fatalf("relocation must be rolled back: root=%q want %q", regs[0].Root, oldRoot)
	}
}

// TestCommandProjectRelocate covers confirmation, the busy guard and the move.
func TestCommandProjectRelocate(t *testing.T) {
	commandEnv(t)
	// A running daemon is needed for the busy check to be meaningful; `running`
	// makes CollectionIdle consult CollectionOpen.
	stub := withStubDaemon(t, &stubDaemon{running: true})
	oldRoot := t.TempDir()
	if err := commandProjectAdd(t.Context(), oldRoot); err != nil {
		t.Fatal(err)
	}
	id := registrations(t)[0].ID

	newRoot := t.TempDir()
	src, err := os.ReadFile(config.ProjectConfigPath(oldRoot))
	if err != nil {
		t.Fatal(err)
	}
	dst := config.ProjectConfigPath(newRoot)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, src, 0o600); err != nil {
		t.Fatal(err)
	}
	// A real move takes the project config away with the directory.
	if err := os.Remove(config.ProjectConfigPath(oldRoot)); err != nil {
		t.Fatal(err)
	}

	// Without --yes nothing happens.
	if err := commandProjectRelocate(t.Context(), newRoot, false); err == nil {
		t.Fatal("relocation without --yes must fail")
	}
	if regs := registrations(t); len(regs) != 1 || regs[0].ID != id {
		t.Fatalf("registrations changed: %v", regs)
	}

	// A collection still open in the daemon blocks the move.
	stub.collection = true
	if err := commandProjectRelocate(t.Context(), newRoot, true); err == nil {
		t.Fatal("relocation of a busy collection must fail")
	}
	if config.NormalizeAbsolutePath(registrations(t)[0].Root) != config.NormalizeAbsolutePath(oldRoot) &&
		registrations(t)[0].Root != oldRoot {
		t.Fatalf("busy relocation must not change the root: %v", registrations(t))
	}

	stub.collection = false
	if err := commandProjectRelocate(t.Context(), newRoot, true); err != nil {
		t.Fatalf("confirmed relocation: %v", err)
	}
	regs := registrations(t)
	if len(regs) != 1 || regs[0].ID != id {
		t.Fatalf("relocation must keep the id: %v", regs)
	}
	if config.NormalizeAbsolutePath(regs[0].Root) != config.NormalizeAbsolutePath(newRoot) {
		t.Fatalf("root=%q want %q", regs[0].Root, newRoot)
	}
}

// TestCommandProjectAddRollbackRestoresHandAuthoredConfig: the rollback must not
// delete a project.yaml the command did not create.
func TestCommandProjectAddRollbackRestoresHandAuthoredConfig(t *testing.T) {
	commandEnv(t)
	root := t.TempDir()
	// A hand-authored project.yaml with overrides, no registration yet.
	cfgPath := config.ProjectConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "schema_version: 1\ncollection_id: 11111111-2222-4333-8444-555555555555\nindexing:\n  skip_dirs: [vendor]\n"
	if err := os.WriteFile(cfgPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	withStubDaemon(t, &stubDaemon{reloadErr: daemonclient.ErrReloadConflict})
	if err := commandProjectAdd(t.Context(), root); err == nil {
		t.Fatal("a reload conflict must surface as an error")
	}

	// The hand-authored file survives byte-for-byte.
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("hand-authored project.yaml was deleted: %v", err)
	}
	if string(got) != original {
		t.Fatalf("project.yaml changed:\n got: %q\nwant: %q", got, original)
	}
	if regs := registrations(t); len(regs) != 0 {
		t.Fatalf("registration must be rolled back: %v", regs)
	}
}

// TestCommandProjectNewCollectionRollsBackOnReloadConflict: re-identifying a root
// while the daemon refuses the snapshot must restore the previous id, otherwise
// every proxy call for that project returns "unknown workspace".
func TestCommandProjectNewCollectionRollsBackOnReloadConflict(t *testing.T) {
	commandEnv(t)
	withStubDaemon(t, &stubDaemon{})
	root := t.TempDir()
	if err := commandProjectAdd(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	originalID := registrations(t)[0].ID
	originalCfg, err := os.ReadFile(config.ProjectConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}

	withStubDaemon(t, &stubDaemon{reloadErr: daemonclient.ErrReloadConflict})
	err = commandProjectNewCollection(t.Context(), root)
	if err == nil {
		t.Fatal("a reload conflict must surface as an error")
	}
	if !errors.Is(err, daemonclient.ErrReloadConflict) {
		t.Fatalf("err=%v want ErrReloadConflict", err)
	}

	regs := registrations(t)
	if len(regs) != 1 || regs[0].ID != originalID {
		t.Fatalf("registration must keep the original id: %v", regs)
	}
	cfgAfter, err := os.ReadFile(config.ProjectConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(cfgAfter) != string(originalCfg) {
		t.Fatalf("project.yaml must be restored:\n got: %q\nwant: %q", cfgAfter, originalCfg)
	}
}

// TestManagementCommandsAreDetected pins the flag routing: any management flag
// selects command mode instead of a server mode.
func TestManagementCommandsAreDetected(t *testing.T) {
	cases := []projectFlags{
		{ProjectAdd: "/p"},
		{ProjectRemove: "/p"},
		{ProjectNew: "/p"},
		{ProjectRelocate: "/p"},
		{ProjectsReload: true},
		{EnsureDaemon: true},
		{StopDaemon: true},
	}
	for _, in := range cases {
		if !managementCommands(in) {
			t.Fatalf("flags %+v not detected as a management command", in)
		}
	}
	if managementCommands(projectFlags{StdioProxy: true, ProjectConfig: "/p/project.yaml"}) {
		t.Fatal("the project proxy is a run mode, not a management command")
	}
}

// TestRunProjectCommandsExitCodes: a failing command returns a non-zero exit code.
func TestRunProjectCommandsExitCodes(t *testing.T) {
	commandEnv(t)
	withStubDaemon(t, &stubDaemon{stopErr: errors.New("no daemon running")})
	if code := runProjectCommands(context.Background(), projectFlags{ProjectAdd: filepath.Join(t.TempDir(), "missing")}); code == 0 {
		t.Fatal("adding a nonexistent root must fail")
	}
	if code := runProjectCommands(context.Background(), projectFlags{StopDaemon: true}); code == 0 {
		t.Fatal("a failing stop must return non-zero")
	}
	withStubDaemon(t, &stubDaemon{})
	if code := runProjectCommands(context.Background(), projectFlags{EnsureDaemon: true}); code != 0 {
		t.Fatalf("ensure-daemon exit code=%d", code)
	}
	// With a daemon running, reload succeeds; without one it must fail, because
	// nothing was reloaded.
	withStubDaemon(t, &stubDaemon{running: true})
	if code := runProjectCommands(context.Background(), projectFlags{ProjectsReload: true}); code != 0 {
		t.Fatalf("projects-reload exit code=%d", code)
	}
	withStubDaemon(t, &stubDaemon{running: false})
	if code := runProjectCommands(context.Background(), projectFlags{ProjectsReload: true}); code == 0 {
		t.Fatal("projects-reload without a running daemon must fail")
	}
}
