package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemonclient"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/testutil"
)

const guiTestGlobalConfig = `active_profile: test
profiles:
  test:
    provider: openai_compatible
    model: mock
    base_url: http://127.0.0.1:9/v1
    dimensions: 8
file_watcher:
  enabled: false
`

// guiFixture registers n projects in a temporary user-level install.
type guiFixture struct {
	t    *testing.T
	user string
}

func newGUIFixture(t *testing.T) *guiFixture {
	t.Helper()
	user := t.TempDir()
	// Registry writes leave files in projects.d; on Windows the parent removal can
	// briefly fail with ERROR_DIR_NOT_EMPTY (pending-delete bookkeeping).
	t.Cleanup(func() { testutil.RemoveDirWithRetry(t, user) })
	t.Setenv("MCP_GLOBAL_DIR", user)
	if err := os.WriteFile(filepath.Join(user, "config.yaml"), []byte(guiTestGlobalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return &guiFixture{t: t, user: user}
}

func (f *guiFixture) addProject(id, description string) string {
	f.t.Helper()
	root := f.t.TempDir()
	cfgDir := filepath.Join(root, config.DefaultInstallDirName)
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	body := "schema_version: 1\ncollection_id: " + id + "\ndescription: \"" + description + "\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, config.ProjectFileName), []byte(body), 0o600); err != nil {
		f.t.Fatal(err)
	}
	dir := filepath.Join(f.user, "projects.d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	reg := "id: " + id + "\nroot: " + root + "\ndescription: \"" + description + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(reg), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return root
}

// fakeService records the collection each call was made for.
type fakeService struct {
	id      string
	mu      sync.Mutex
	queries []string
}

func (f *fakeService) SemanticSearch(ctx context.Context, req service.SearchRequest) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.queries = append(f.queries, req.Query)
	f.mu.Unlock()
	return json.RawMessage(`{"workspace_id":"` + f.id + `","results":[]}`), nil
}

func (f *fakeService) GetIndexStatus(ctx context.Context) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"workspace_id":"` + f.id + `"}`), nil
}

func (f *fakeService) Reindex(ctx context.Context, req service.ReindexRequest) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"workspace_id":"` + f.id + `","force":` + boolJSON(req.Force) + `}`), nil
}

func (f *fakeService) CheckUpdate(ctx context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (f *fakeService) Ready(ctx context.Context) error { return ctx.Err() }

func boolJSON(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// TestCollectionsList: the GUI shows the registry read-only, sorted, and never
// invents a collection when the registry is empty.
func TestCollectionsList(t *testing.T) {
	f := newGUIFixture(t)
	collections := &Collections{Manager: daemon.NewProjectManager(f.user)}

	list, err := collections.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("empty registry must list nothing: %+v", list)
	}

	f.addProject("col-b", "Beta")
	f.addProject("col-a", "Alpha")
	list, err = collections.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list=%+v", list)
	}
	if list[0].Description != "Alpha" || list[1].Description != "Beta" {
		t.Fatalf("list not sorted: %+v", list)
	}
	if list[0].Label() == list[0].ID {
		t.Fatalf("label should include the description: %q", list[0].Label())
	}
}

// TestCollectionsEmptyRegistryIsActionable: an empty registry returns a
// dedicated error the UI can turn into "add a project" instructions.
func TestCollectionsEmptyRegistryIsActionable(t *testing.T) {
	f := newGUIFixture(t)
	_, _, _, err := OpenGUI(t.Context(), f.user, &Session{})
	if !errors.Is(err, ErrNoCollections) {
		t.Fatalf("err=%v want ErrNoCollections", err)
	}
}

// TestSessionSwitchChangesTargetAndCancelsPrevious pins the GUI contract: the
// selected collection is the one requests go to, and switching cancels the
// previous collection's scope so its results cannot leak into the new view.
func TestSessionSwitchChangesTargetAndCancelsPrevious(t *testing.T) {
	f := newGUIFixture(t)
	f.addProject("col-a", "Alpha")
	f.addProject("col-b", "Beta")

	services := map[string]*fakeService{}
	session := &Session{
		Connect: func(context.Context) (daemonclient.Connection, error) {
			return daemonclient.Connection{BaseURL: "http://127.0.0.1:8080", Token: "t"}, nil
		},
		NewService: func(_ daemonclient.Connection, collectionID string, _ string) service.Service {
			svc := &fakeService{id: collectionID}
			services[collectionID] = svc
			return svc
		},
	}
	defer session.Close()

	svcA, err := session.Open(t.Context(), "col-a")
	if err != nil {
		t.Fatal(err)
	}
	scopeA := session.Context()
	if session.Current() != "col-a" {
		t.Fatalf("current=%q", session.Current())
	}
	raw, err := svcA.GetIndexStatus(scopeA)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"workspace_id":"col-a"}` {
		t.Fatalf("A status=%s", raw)
	}

	svcB, err := session.Open(t.Context(), "col-b")
	if err != nil {
		t.Fatal(err)
	}
	if session.Current() != "col-b" {
		t.Fatalf("current=%q", session.Current())
	}
	// The previous collection's scope is cancelled: its in-flight requests abort.
	if err := scopeA.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("previous scope err=%v want context.Canceled", err)
	}
	if _, err := svcA.GetIndexStatus(scopeA); err == nil {
		t.Fatal("a request on the previous collection's scope must fail")
	}
	// The new collection works on its own scope.
	raw, err = svcB.GetIndexStatus(session.Context())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"workspace_id":"col-b"}` {
		t.Fatalf("B status=%s", raw)
	}
	// No results or progress crossed over.
	if len(services["col-a"].queries) != 0 || len(services["col-b"].queries) != 0 {
		t.Fatal("search results must not be carried across a switch")
	}
}

// TestSessionRequiresDaemon: without a daemon the GUI reports the error and does
// not pretend to have a collection.
func TestSessionRequiresDaemon(t *testing.T) {
	session := &Session{
		Connect: func(context.Context) (daemonclient.Connection, error) {
			return daemonclient.Connection{}, errors.New("daemon down")
		},
	}
	defer session.Close()
	if _, err := session.Open(t.Context(), "col-a"); err == nil {
		t.Fatal("expected an error when the daemon is unavailable")
	}
	if session.Current() != "" {
		t.Fatalf("current=%q", session.Current())
	}
}

// TestSessionRejectsEmptyID: an empty selection is a programming error, not a
// silent fallback to "the first collection".
func TestSessionRejectsEmptyID(t *testing.T) {
	session := &Session{}
	defer session.Close()
	if _, err := session.Open(t.Context(), ""); err == nil {
		t.Fatal("empty collection id must be rejected")
	}
}

// TestSessionCloseCancelsScope: closing the window cancels outstanding requests
// but never stops the shared daemon.
func TestSessionCloseCancelsScope(t *testing.T) {
	session := &Session{
		Connect: func(context.Context) (daemonclient.Connection, error) {
			return daemonclient.Connection{BaseURL: "http://127.0.0.1:8080"}, nil
		},
		NewService: func(_ daemonclient.Connection, id string, _ string) service.Service {
			return &fakeService{id: id}
		},
	}
	if _, err := session.Open(t.Context(), "col-a"); err != nil {
		t.Fatal(err)
	}
	scope := session.Context()
	session.Close()
	if err := scope.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("scope err=%v", err)
	}
	if session.Current() != "" {
		t.Fatalf("current=%q", session.Current())
	}
}

// TestGUINeverBuildsPhase1 is the structural guard for the plan's requirement
// that the GUI is a daemon client: the global GUI path must not construct a
// Phase1 or open zvec itself.
func TestGUINeverBuildsPhase1(t *testing.T) {
	data, err := os.ReadFile("collections.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, forbidden := range []string{"NewPhase1", "zvec.New(", "PrepareWorkspaceLocks"} {
		if containsText(body, forbidden) {
			t.Fatalf("collections.go must not use %s: the GUI is a daemon client", forbidden)
		}
	}
	if !containsText(body, "HTTPProxy") {
		t.Fatal("the GUI must reach collections through the HTTP proxy")
	}
}

// TestOpenGUIOpensFirstCollection: the entry point opens a collection so the
// window never starts with an empty selection.
func TestOpenGUIOpensFirstCollection(t *testing.T) {
	f := newGUIFixture(t)
	f.addProject("col-a", "Alpha")
	f.addProject("col-b", "Beta")

	session := &Session{
		Connect: func(context.Context) (daemonclient.Connection, error) {
			return daemonclient.Connection{BaseURL: "http://127.0.0.1:8080"}, nil
		},
		NewService: func(_ daemonclient.Connection, id string, _ string) service.Service {
			return &fakeService{id: id}
		},
	}
	defer session.Close()

	list, selected, svc, err := OpenGUI(t.Context(), f.user, session)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || selected == nil || selected.ID != "col-a" {
		t.Fatalf("list=%+v selected=%+v", list, selected)
	}
	if svc == nil {
		t.Fatal("no service returned")
	}
	if _, err := svc.GetIndexStatus(session.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestSessionScopeOutlivesRequestContext: a request must be able to use the
// session scope even after the UI call that created it returned, otherwise the
// 2-second status poll would cancel itself.
func TestSessionScopeOutlivesRequestContext(t *testing.T) {
	session := &Session{
		Connect: func(context.Context) (daemonclient.Connection, error) {
			return daemonclient.Connection{BaseURL: "http://127.0.0.1:8080"}, nil
		},
		NewService: func(_ daemonclient.Connection, id string, _ string) service.Service {
			return &fakeService{id: id}
		},
	}
	defer session.Close()

	openCtx, cancelOpen := context.WithCancel(context.Background())
	if _, err := session.Open(openCtx, "col-a"); err != nil {
		t.Fatal(err)
	}
	cancelOpen()
	if err := session.Context().Err(); err != nil {
		t.Fatalf("session scope must survive the Open call context: %v", err)
	}

	ctx, cancel := context.WithTimeout(session.Context(), time.Second)
	defer cancel()
	if _, err := session.Context(), ctx.Err(); err != nil {
		t.Fatal(err)
	}
}

// TestRequireServiceRejectsNil: the GUI dereferences its service on the first
// status poll, so a nil service must be reported before the window is shown
// instead of crashing inside a goroutine.
func TestRequireServiceRejectsNil(t *testing.T) {
	if err := RequireService(nil, nil); err == nil {
		t.Fatal("a nil service must be rejected")
	}
	if err := RequireService(nil, &Session{}); err == nil {
		t.Fatal("a nil service with an empty session must be rejected")
	}
	// A real service is accepted.
	svc := &fakeService{id: "col-a"}
	if err := RequireService(svc, nil); err != nil {
		t.Fatalf("a non-nil service must be accepted: %v", err)
	}
}

// TestRequireServiceMentionsSelectedCollection: the error tells the user which
// collection the daemon stopped serving.
func TestRequireServiceMentionsSelectedCollection(t *testing.T) {
	session := &Session{
		Connect: func(context.Context) (daemonclient.Connection, error) {
			return daemonclient.Connection{BaseURL: "http://127.0.0.1:8080"}, nil
		},
		NewService: func(_ daemonclient.Connection, id string, _ string) service.Service {
			return &fakeService{id: id}
		},
	}
	defer session.Close()
	if _, err := session.Open(t.Context(), "col-xyz"); err != nil {
		t.Fatal(err)
	}
	err := RequireService(nil, session)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !containsText(err.Error(), "col-xyz") {
		t.Fatalf("error should name the collection: %v", err)
	}
}

// TestSessionDefaultConnectCanLaunchDaemon guards the production default path: with
// no injected Connect, the session must build a client that can actually start the
// daemon. Without an Executable the launcher fails with "daemon executable path is
// required", so the GUI could never recover after a reboot or --stop-daemon.
func TestSessionDefaultConnectCanLaunchDaemon(t *testing.T) {
	f := newGUIFixture(t)
	f.addProject("col-a", "Alpha")

	// A free port with no listener, so EnsureRunning must go through the launcher.
	port, err := freeLoopbackPortForTest()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.user, "daemon.yaml"),
		[]byte(fmt.Sprintf("http_addr: 127.0.0.1:%d\n", port)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := daemonclient.EnsureAuthToken(); err != nil {
		t.Fatal(err)
	}

	// Observe the launcher instead of spawning a detached daemon.
	prevStarter := guiDaemonStarter
	prevExecutable := guiExecutable
	t.Cleanup(func() {
		guiDaemonStarter = prevStarter
		guiExecutable = prevExecutable
	})
	var gotExecutable string
	guiDaemonStarter = func(_ context.Context, opts daemonclient.LaunchOptions) error {
		gotExecutable = opts.Executable
		return errors.New("launcher reached")
	}
	guiExecutable = func() string { return "/opt/mcp/mcp-semantic-search-zvec-go" }

	session := &Session{}
	defer session.Close()
	_, err = session.Open(t.Context(), "col-a")
	if err == nil {
		t.Fatal("expected the injected launcher to abort the start")
	}
	if !strings.Contains(err.Error(), "launcher reached") {
		t.Fatalf("the default client never reached the launcher: %v", err)
	}
	if gotExecutable != "/opt/mcp/mcp-semantic-search-zvec-go" {
		t.Fatalf("the default client passed Executable=%q; the daemon could never be launched", gotExecutable)
	}
}

// TestSessionDefaultExecutableIsReal checks that the production resolver returns a
// usable path: an empty or nonexistent Executable makes the launcher fail.
func TestSessionDefaultExecutableIsReal(t *testing.T) {
	got := currentExecutable()
	if got == "" {
		t.Fatal("currentExecutable returned an empty path")
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("currentExecutable returned %q, which does not exist: %v", got, err)
	}
}

func freeLoopbackPortForTest() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return port, listener.Close()
}

func containsText(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
