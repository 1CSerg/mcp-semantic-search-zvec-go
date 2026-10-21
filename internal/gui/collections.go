package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemonclient"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
)

// This file separates the GUI's backend from its UI: the collection list and the
// per-collection service are plain Go, testable on every platform. In the global
// layout the GUI is a client of the one shared daemon, exactly like an MCP proxy:
// it never builds a Phase1 and never opens zvec itself.

// ErrNoCollections reports an empty registry. The GUI shows instructions to add a
// project; it never invents a collection.
var ErrNoCollections = errors.New("no projects are registered yet")

// Collection is one selectable project.
type Collection struct {
	ID          string
	Root        string
	Description string
}

// Label renders the collection for a picker.
func (c Collection) Label() string {
	if c.Description != "" && c.Description != c.ID {
		return fmt.Sprintf("%s — %s", c.Description, c.ID)
	}
	return c.ID
}

// Collections is the read-only project list shown by the GUI.
type Collections struct {
	// Manager supplies the registrations. Injectable for tests.
	Manager *daemon.ProjectManager
}

// List returns the registered collections, sorted by description then id.
func (c *Collections) List() ([]Collection, error) {
	if c.Manager == nil {
		return nil, errors.New("gui: no project manager configured")
	}
	regs, err := c.Manager.Registrations()
	if err != nil {
		return nil, err
	}
	out := make([]Collection, 0, len(regs))
	for _, reg := range regs {
		out = append(out, Collection{ID: reg.ID, Root: reg.Root, Description: reg.Description})
	}
	sortCollections(out)
	return out, nil
}

func sortCollections(list []Collection) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0; j-- {
			a, b := list[j-1], list[j]
			if a.Description < b.Description || (a.Description == b.Description && a.ID <= b.ID) {
				break
			}
			list[j-1], list[j] = b, a
		}
	}
}

// Session is the GUI's view of one collection: a proxy to the shared daemon plus
// the cancellation scope of the requests issued for it.
//
// Switching collections cancels the previous scope, so a slow search for
// guiDaemonStarter launches the shared daemon. It is a variable so a test can
// observe the production default path without spawning a detached process.
var guiDaemonStarter = daemonclient.StartDaemon

// guiExecutable resolves the binary to launch. A variable for the same reason.
var guiExecutable = currentExecutable

// collection A can never paint its results into collection B's view.
type Session struct {
	mu      sync.Mutex
	current *collectionSession
	// Connect obtains a daemon connection. Injectable for tests.
	Connect func(ctx context.Context) (daemonclient.Connection, error)
	// NewService builds the service for one collection. Injectable for tests.
	NewService func(conn daemonclient.Connection, collectionID string, activeProfile string) service.Service
}

type collectionSession struct {
	collection Collection
	svc        service.Service
	ctx        context.Context
	cancel     context.CancelFunc
	conn       daemonclient.Connection
}

// Open switches the GUI to collectionID and returns its service. The previous
// collection's in-flight requests are cancelled first.
func (s *Session) Open(ctx context.Context, collectionID string) (service.Service, error) {
	if collectionID == "" {
		return nil, errors.New("gui: collection id is required")
	}
	connect := s.Connect
	if connect == nil {
		// The GUI is a client of the shared daemon and must be able to start it
		// after a reboot or an explicit stop: without Executable the launcher has
		// nothing to run.
		client := &daemonclient.Client{
			Executable: guiExecutable(),
			Start:      guiDaemonStarter,
		}
		connect = client.EnsureRunning
	}
	conn, err := connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("shared daemon unavailable: %w", err)
	}
	newService := s.NewService
	if newService == nil {
		newService = defaultGUIService
	}
	svc := newService(conn, collectionID, activeProfileFor(collectionID))

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		// Cancel the previous collection's scope: its responses must not land in
		// the new view.
		s.current.cancel()
	}
	scope, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.current = &collectionSession{
		collection: Collection{ID: collectionID},
		svc:        svc,
		ctx:        scope,
		cancel:     cancel,
		conn:       conn,
	}
	return svc, nil
}

// Current returns the selected collection id, or "" when nothing is selected.
func (s *Session) Current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return ""
	}
	return s.current.collection.ID
}

// Context returns the cancellation scope of the selected collection. Requests
// must use it so a collection switch aborts them.
func (s *Session) Context() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return context.Background()
	}
	return s.current.ctx
}

// Close cancels the selected collection's scope. The GUI never stops the shared
// daemon: other IDE sessions may be using it.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		s.current.cancel()
		s.current = nil
	}
}

// defaultGUIService builds the same HTTP proxy an MCP stdio proxy uses. The budget
// comes from the profile the collection runs with, including a project override.
func defaultGUIService(conn daemonclient.Connection, collectionID string, activeProfile string) service.Service {
	timeout := time.Duration(0)
	if path, err := config.GlobalConfigPath(); err == nil {
		if app, err := config.LoadAppConfig(path); err == nil {
			name := app.ActiveProfile
			if strings.TrimSpace(activeProfile) != "" {
				name = activeProfile
			}
			if profile, ok := app.Profiles[name]; ok {
				timeout = config.EmbedHTTPBudget(profile)
			}
		}
	}
	return service.NewHTTPProxyWithTimeout(conn.BaseURL, collectionID, conn.Token, timeout)
}

// activeProfileFor returns the profile the collection runs with: its own override
// when set, otherwise the shared active_profile. Best effort — a registry read
// failure just yields the shared default.
func activeProfileFor(collectionID string) string {
	userDir, err := config.UserDataDir()
	if err != nil {
		return ""
	}
	regs, err := daemon.NewProjectManager(userDir).Registrations()
	if err != nil {
		return ""
	}
	for _, reg := range regs {
		if reg.ID != collectionID {
			continue
		}
		pc, loadErr := config.LoadProjectConfig(config.ProjectConfigPath(reg.Root))
		if loadErr != nil {
			return ""
		}
		return pc.ActiveProfile
	}
	return ""
}

// currentExecutable returns the running binary path, used to launch the daemon.
func currentExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// OpenGUI is what the GUI entry point calls: it lists collections, opens the
// first one and reports an actionable error when the registry is empty.
func OpenGUI(ctx context.Context, userDir string, session *Session) ([]Collection, *Collection, service.Service, error) {
	collections := &Collections{Manager: daemon.NewProjectManager(userDir)}
	list, err := collections.List()
	if err != nil {
		return nil, nil, nil, err
	}
	if len(list) == 0 {
		return nil, nil, nil, ErrNoCollections
	}
	first := list[0]
	svc, err := session.Open(ctx, first.ID)
	if err != nil {
		return list, nil, nil, err
	}
	return list, &first, svc, nil
}

// RequireService reports an error when the GUI has no service to talk to. The UI
// dereferences the service on its first status poll, so a nil service must be
// rejected before the window is shown instead of crashing inside a goroutine.
func RequireService(svc service.Service, session *Session) error {
	if svc != nil {
		return nil
	}
	hint := "run --project-add <project> or --ensure-daemon"
	if session != nil && session.Current() != "" {
		hint = "the shared daemon stopped serving collection " + session.Current() + "; run --ensure-daemon"
	}
	return fmt.Errorf("gui: no collection is available (%s)", hint)
}
