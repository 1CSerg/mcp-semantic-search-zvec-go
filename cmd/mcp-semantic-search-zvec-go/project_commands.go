package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemonclient"
)

// projectCommandTimeout bounds the management commands so a wedged daemon or a
// stuck lock cannot hang an install script forever.
const projectCommandTimeout = 60 * time.Second

// managementCommands reports whether any single-shot project command was asked
// for. These run instead of a server mode.
func managementCommands(in projectFlags) bool {
	return in.ProjectAdd != "" || in.ProjectRemove != "" || in.ProjectNew != "" ||
		in.ProjectRelocate != "" || in.ProjectsReload || in.EnsureDaemon || in.StopDaemon
}

// runProjectCommands executes the requested management command and returns the
// process exit code. Every command reports to stderr; stdout stays reserved for
// MCP frames.
func runProjectCommands(ctx context.Context, in projectFlags) int {
	ctx, cancel := context.WithTimeout(ctx, projectCommandTimeout)
	defer cancel()

	switch {
	case in.ProjectAdd != "":
		return reportProjectCommand("add", func() error { return commandProjectAdd(ctx, in.ProjectAdd) })
	case in.ProjectRemove != "":
		return reportProjectCommand("remove", func() error { return commandProjectRemove(ctx, in.ProjectRemove) })
	case in.ProjectNew != "":
		return reportProjectCommand("new-collection", func() error { return commandProjectNewCollection(ctx, in.ProjectNew) })
	case in.ProjectRelocate != "":
		return reportProjectCommand("relocate", func() error { return commandProjectRelocate(ctx, in.ProjectRelocate, in.Yes) })
	case in.ProjectsReload:
		return reportProjectCommand("reload", func() error { return commandProjectsReload(ctx) })
	case in.EnsureDaemon:
		return reportProjectCommand("ensure-daemon", func() error { return commandEnsureDaemon(ctx) })
	case in.StopDaemon:
		return reportProjectCommand("stop-daemon", func() error { return commandStopDaemon(ctx) })
	}
	return 0
}

func reportProjectCommand(name string, fn func() error) int {
	if err := fn(); err != nil {
		fmt.Fprintf(os.Stderr, "mcp-semantic-search-zvec-go: %s: %v\n", name, err)
		return 1
	}
	return 0
}

// projectManager builds a ProjectManager for the user-level install.
func projectManager() (*daemon.ProjectManager, error) {
	dir, err := config.UserDataDir()
	if err != nil {
		return nil, err
	}
	return daemon.NewProjectManager(dir), nil
}

// daemonClient builds a client that launches the same installed binary.
func daemonClient() *daemonclient.Client {
	return &daemonclient.Client{
		Executable: currentExecutable(),
		Start:      daemonclient.StartDaemon,
	}
}

// daemonAPI is the subset of daemonclient.Client the management commands use.
// Declaring it as an interface keeps the orchestration unit-testable without
// spawning a real detached daemon.
type daemonAPI interface {
	EnsureRunning(ctx context.Context) (daemonclient.Connection, error)
	Stop(ctx context.Context) error
	Running(ctx context.Context) (bool, error)
	ReloadProjects(ctx context.Context) error
	CollectionOpen(ctx context.Context, collectionID string) (bool, error)
}

// Test seams: the management commands are orchestration, so their daemon client
// and project manager are injectable and unit tests never spawn a real daemon.
var (
	newDaemonClient   = func() daemonAPI { return daemonClient() }
	newProjectManager = projectManager
)

// commandProjectAdd registers a project, ensures the daemon and reloads it.
//
// The mutation and the reload are one operation: if the daemon refuses the new
// snapshot (a busy collection, 409), this command's own write is undone so the
// on-disk registry never advertises a project the daemon is not serving. A
// pre-existing project.yaml is restored, not deleted: the command did not create it.
func commandProjectAdd(ctx context.Context, root string) error {
	m, err := newProjectManager()
	if err != nil {
		return err
	}
	before, err := captureProjectSnapshot(m, root)
	if err != nil {
		return err
	}
	pc, err := m.AddProject(root)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "project %s registered as collection %s\n", root, pc.CollectionID)

	if err := ensureDaemonAndReload(ctx); err != nil {
		if errors.Is(err, daemonclient.ErrReloadConflict) && before.registration == nil {
			// Nothing was registered before: undo the whole write, restoring a
			// project.yaml this command did not author.
			if rbErr := before.restore(m); rbErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not roll back registration: %v\n", rbErr)
			} else {
				fmt.Fprintf(os.Stderr, "registration rolled back: %v\n", err)
			}
		}
		return fmt.Errorf("project registered but the daemon was not reloaded: %w", err)
	}
	return nil
}

// registrationForRoot returns the current registration for a root, if any.
func registrationForRoot(m *daemon.ProjectManager, root string) (*daemon.Registration, error) {
	regs, err := m.Registrations()
	if err != nil {
		return nil, err
	}
	want := config.NormalizeAbsolutePath(root)
	for i := range regs {
		if config.NormalizeAbsolutePath(regs[i].Root) == want {
			return &regs[i], nil
		}
	}
	return nil, nil
}

// projectSnapshot is what a management command must restore to undo its own
// mutation when the daemon refuses the new snapshot (409). Restoring only the
// registration would leave a project without its project.yaml, so both are kept.
type projectSnapshot struct {
	registration *daemon.Registration
	projectYAML  []byte
	// hadProjectYAML records whether the file existed, so restore can delete a file
	// the command created instead of leaving a stray one behind.
	hadProjectYAML bool
	root           string
}

func captureProjectSnapshot(m *daemon.ProjectManager, root string) (*projectSnapshot, error) {
	reg, err := registrationForRoot(m, root)
	if err != nil {
		return nil, err
	}
	snap := &projectSnapshot{root: root, registration: reg}
	data, readErr := os.ReadFile(config.ProjectConfigPath(root))
	if readErr == nil {
		snap.projectYAML = data
		snap.hadProjectYAML = true
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	}
	return snap, nil
}

// restore puts the registration and project.yaml back. It is best effort per
// file: a partial restore is reported, never silently ignored.
//
// It first drops whatever registration this root currently has, so re-identifying
// a root (--project-new-collection) does not leave two registrations for one root,
// which the loader would reject.
func (s *projectSnapshot) restore(m *daemon.ProjectManager) error {
	var problems []string
	regs, err := m.Registrations()
	if err != nil {
		return err
	}
	want := config.NormalizeAbsolutePath(s.root)
	for _, reg := range regs {
		if config.NormalizeAbsolutePath(reg.Root) != want {
			continue
		}
		if s.registration != nil && strings.EqualFold(reg.ID, s.registration.ID) {
			continue
		}
		if err := m.RemoveRegistrationOnly(reg.ID); err != nil {
			problems = append(problems, fmt.Sprintf("stale registration %s: %v", reg.ID, err))
		}
	}
	if s.registration != nil {
		if err := m.RestoreRegistration(daemon.Registration{
			ID:          s.registration.ID,
			Root:        s.registration.Root,
			Description: s.registration.Description,
		}); err != nil {
			problems = append(problems, fmt.Sprintf("registration: %v", err))
		}
	}
	cfgPath := config.ProjectConfigPath(s.root)
	switch {
	case s.hadProjectYAML:
		if err := writeFileAtomicForCommand(cfgPath, s.projectYAML); err != nil {
			problems = append(problems, fmt.Sprintf("project.yaml: %v", err))
		}
	default:
		// The command created it; remove it again.
		if err := os.Remove(cfgPath); err != nil && !os.IsNotExist(err) {
			problems = append(problems, fmt.Sprintf("project.yaml: %v", err))
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// commandProjectRemove drops the registration and the managed project files.
// Collection data is preserved.
func commandProjectRemove(ctx context.Context, root string) error {
	m, err := newProjectManager()
	if err != nil {
		return err
	}
	snap, err := captureProjectSnapshot(m, root)
	if err != nil {
		return err
	}
	if snap.registration == nil {
		// Removing an unregistered root is a no-op.
		if err := m.RemoveProject(root); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "project %s was not registered\n", root)
		return nil
	}
	if err := m.RemoveProject(root); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "project %s unregistered (collection data kept)\n", root)

	if err := reloadRunningDaemon(ctx); err != nil {
		if errors.Is(err, daemonclient.ErrReloadConflict) {
			// The daemon still serves this collection, so the removal must not
			// stand: put the registration and project.yaml back.
			if rbErr := snap.restore(m); rbErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not roll back removal: %v\n", rbErr)
			} else {
				fmt.Fprintf(os.Stderr, "removal rolled back: %v\n", err)
			}
		}
		return fmt.Errorf("project unregistered but the daemon was not reloaded: %w", err)
	}
	return nil
}

// writeFileAtomicForCommand writes data via a temp file in the same directory and
// a rename, so a rollback can never leave a truncated project.yaml behind.
func writeFileAtomicForCommand(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// commandProjectNewCollection gives a project copy its own collection id.
//
// Re-identifying an existing root drops the previous registration, so a refused
// snapshot (409, the old collection still open) must put both files back and fail:
// leaving project.yaml on a new id while the daemon serves the old one makes every
// proxy call for that project return "unknown workspace".
func commandProjectNewCollection(ctx context.Context, root string) error {
	m, err := newProjectManager()
	if err != nil {
		return err
	}
	before, err := captureProjectSnapshot(m, root)
	if err != nil {
		return err
	}
	pc, err := m.NewProjectCollection(root)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "project %s now uses collection %s (new index will be built on first use)\n",
		root, pc.CollectionID)

	if err := ensureDaemonAndReload(ctx); err != nil {
		if errors.Is(err, daemonclient.ErrReloadConflict) {
			if rbErr := before.restore(m); rbErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not roll back the new collection id: %v\n", rbErr)
			} else {
				fmt.Fprintf(os.Stderr, "new collection id rolled back: %v\n", err)
			}
		}
		return fmt.Errorf("collection id changed but the daemon was not reloaded: %w", err)
	}
	return nil
}

// commandProjectRelocate moves a registered collection to a new root, keeping its
// id. It requires explicit confirmation and refuses while the collection is busy.
func commandProjectRelocate(ctx context.Context, newRoot string, confirmed bool) error {
	m, err := newProjectManager()
	if err != nil {
		return err
	}
	client := newDaemonClient()

	// Remember where the collection lived so a refused snapshot can be undone.
	var previous *daemon.Registration
	pc, readErr := config.LoadProjectConfig(config.ProjectConfigPath(newRoot))
	if readErr == nil {
		regs, regsErr := m.Registrations()
		if regsErr != nil {
			return regsErr
		}
		for i := range regs {
			if strings.EqualFold(regs[i].ID, pc.CollectionID) {
				previous = &regs[i]
				break
			}
		}
	}

	_, err = m.RelocateProject(newRoot, daemon.RelocateOptions{
		Confirmed: confirmed,
		CanRelocate: func(collectionID string) error {
			return collectionIdle(ctx, client, collectionID)
		},
	})
	if err != nil {
		if errors.Is(err, daemon.ErrCollectionBusy) || strings.Contains(err.Error(), "busy") {
			return fmt.Errorf("%w; close MCP sessions for this project and retry", err)
		}
		return err
	}
	fmt.Fprintf(os.Stderr, "project relocated to %s\n", newRoot)

	if err := reloadRunningDaemon(ctx); err != nil {
		if errors.Is(err, daemonclient.ErrReloadConflict) && previous != nil {
			// The daemon refused the snapshot, so the old root is still the served
			// one: put the registration back instead of leaving them disagreeing.
			if rbErr := m.RestoreRegistration(*previous); rbErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not roll back relocation: %v\n", rbErr)
			} else {
				fmt.Fprintf(os.Stderr, "relocation rolled back: %v\n", err)
			}
		}
		return fmt.Errorf("project relocated but the daemon was not reloaded: %w", err)
	}
	return nil
}

// collectionIdle reports whether the daemon currently has the collection open.
// A daemon that is not running means nothing is open; any other failure is
// propagated, because a broken check must not silently allow a relocation.
func collectionIdle(ctx context.Context, client daemonAPI, collectionID string) error {
	running, err := client.Running(ctx)
	if err != nil {
		return fmt.Errorf("cannot check whether collection %q is open: %w", collectionID, err)
	}
	if !running {
		return nil
	}
	open, err := client.CollectionOpen(ctx, collectionID)
	if err != nil {
		return fmt.Errorf("cannot check whether collection %q is open: %w", collectionID, err)
	}
	if open {
		return fmt.Errorf("collection %q is open in the daemon", collectionID)
	}
	return nil
}

// commandProjectsReload asks the running daemon to re-read projects.d. With no
// daemon running nothing is reloaded, so that is reported as an error rather than
// a misleading "reloaded" success.
func commandProjectsReload(ctx context.Context) error {
	client := newDaemonClient()
	running, err := client.Running(ctx)
	if err != nil {
		return err
	}
	if !running {
		return errors.New("no daemon is running; nothing to reload (start it with --ensure-daemon)")
	}
	if err := client.ReloadProjects(ctx); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "project registry reloaded")
	return nil
}

// commandEnsureDaemon starts the daemon if it is not running.
func commandEnsureDaemon(ctx context.Context) error {
	conn, err := newDaemonClient().EnsureRunning(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "daemon is running at %s\n", conn.BaseURL)
	return nil
}

// commandStopDaemon asks the daemon to shut down gracefully.
func commandStopDaemon(ctx context.Context) error {
	if err := newDaemonClient().Stop(ctx); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "daemon stopped")
	return nil
}

// ensureDaemonAndReload starts the daemon if needed, then reloads the registry so
// a newly added project is available without restarting anything.
func ensureDaemonAndReload(ctx context.Context) error {
	client := newDaemonClient()
	if _, err := client.EnsureRunning(ctx); err != nil {
		return err
	}
	return client.ReloadProjects(ctx)
}

// reloadRunningDaemon reloads only when a daemon is already running: management
// commands must not start one as a side effect.
func reloadRunningDaemon(ctx context.Context) error {
	client := newDaemonClient()
	running, err := client.Running(ctx)
	if err != nil {
		return err
	}
	if !running {
		return nil
	}
	return client.ReloadProjects(ctx)
}

// resolveProjectRoot turns a CLI path into an absolute cleaned path.
func resolveProjectRoot(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("a project root is required")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	return abs, nil
}
