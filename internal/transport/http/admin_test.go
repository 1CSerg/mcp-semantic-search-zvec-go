package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
)

const adminTestGlobalConfig = `active_profile: test
profiles:
  test:
    provider: openai_compatible
    model: mock
    base_url: http://127.0.0.1:9/v1
    dimensions: 8
file_watcher:
  enabled: false
`

// adminFixture builds a user-level install with a projects.d registry and an
// authenticated daemon HTTP server wired to a reloadable registry.
type adminFixture struct {
	t     *testing.T
	user  string
	token string
	srv   *Server
	reg   *daemon.Registry
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	user := t.TempDir()
	t.Setenv("MCP_GLOBAL_DIR", user)
	if err := os.WriteFile(filepath.Join(user, "config.yaml"), []byte(adminTestGlobalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	// Auto-index off so a cold open does not leave a running job that would make
	// the collection permanently "busy" for reload assertions.
	if err := os.WriteFile(filepath.Join(user, "daemon.yaml"),
		[]byte("auto_index_on_open: false\nmax_open_workspaces: 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := testSettings()
	settings.APIToken = "admin-token"
	// Reload is strict in production (daemon.ConfigLoader), so the fixture uses the
	// strict loader: a partially valid snapshot must be rejected with 400.
	loader := func() (daemon.Config, error) {
		return daemon.LoadConfigStrict("")
	}
	cfg, err := loader()
	if err != nil {
		t.Fatal(err)
	}
	reg := daemon.NewRegistry(cfg, t.Context())
	reg.SetConfigLoader(loader)
	t.Cleanup(reg.Close)

	f := &adminFixture{t: t, user: user, token: settings.APIToken, reg: reg}
	f.srv = NewDaemon(settings, reg)
	return f
}

func (f *adminFixture) addProject(id string) string {
	f.t.Helper()
	root := f.t.TempDir()
	cfgDir := filepath.Join(root, config.DefaultInstallDirName)
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	body := "schema_version: 1\ncollection_id: " + id + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, config.ProjectFileName), []byte(body), 0o600); err != nil {
		f.t.Fatal(err)
	}
	dir := filepath.Join(f.user, "projects.d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	reg := "id: " + id + "\nroot: " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(reg), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return root
}

func (f *adminFixture) do(method, path, body string, auth bool) *httptest.ResponseRecorder {
	f.t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	var req *http.Request
	if reader != nil {
		req = httptest.NewRequest(method, path, reader)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	return rec
}

// TestAdminReloadRequiresBearer pins that admin routes are never open: without a
// configured token they are disabled, and with one they demand a valid Bearer.
func TestAdminReloadRequiresBearer(t *testing.T) {
	f := newAdminFixture(t)
	f.addProject("col-a")

	if rec := f.do(http.MethodPost, "/v1/admin/reload-projects", "", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer: status=%d body=%s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/reload-projects", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer: status=%d", rec.Code)
	}

	// Without an API token the admin API is disabled, not unauthenticated-open.
	noToken := testSettings()
	srv := NewDaemon(noToken, f.reg)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/admin/reload-projects", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no configured token: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAdminReloadProjectsApplies checks the success path: the response is 200
// with ok=true and the new collection is served afterwards.
func TestAdminReloadProjectsApplies(t *testing.T) {
	f := newAdminFixture(t)
	f.addProject("col-a")
	if rec := f.do(http.MethodPost, "/v1/admin/reload-projects", "", true); rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	f.addProject("col-b")
	rec := f.do(http.MethodPost, "/v1/admin/reload-projects", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		OK          bool `json:"ok"`
		Collections int  `json:"collections"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Collections != 2 {
		t.Fatalf("payload=%+v", payload)
	}
	if _, err := f.reg.GetService("col-b"); err != nil {
		t.Fatalf("B must be served after reload: %v", err)
	}
}

// TestAdminReloadRejectsBody: reload takes no parameters, so a body that tries to
// supply a root is rejected rather than honoured.
func TestAdminReloadRejectsBody(t *testing.T) {
	f := newAdminFixture(t)
	f.addProject("col-a")
	rec := f.do(http.MethodPost, "/v1/admin/reload-projects", `{"root":"/tmp/evil"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// An empty object is tolerated: it carries no parameters.
	if rec := f.do(http.MethodPost, "/v1/admin/reload-projects", `{}`, true); rec.Code != http.StatusOK {
		t.Fatalf("empty object: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAdminReloadInvalidSnapshot: a malformed registration yields 400 and leaves
// the running snapshot intact.
func TestAdminReloadInvalidSnapshot(t *testing.T) {
	f := newAdminFixture(t)
	f.addProject("col-a")
	if rec := f.do(http.MethodPost, "/v1/admin/reload-projects", "", true); rec.Code != http.StatusOK {
		t.Fatalf("initial reload: status=%d", rec.Code)
	}
	if err := os.WriteFile(filepath.Join(f.user, "projects.d", "broken.yaml"), []byte("id: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := f.do(http.MethodPost, "/v1/admin/reload-projects", "", true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := f.reg.GetService("col-a"); err != nil {
		t.Fatalf("A must keep serving after an invalid snapshot: %v", err)
	}
}

// TestAdminReloadBusyCollection: a changed collection with in-flight work yields
// 409 and nothing is applied.
func TestAdminReloadBusyCollection(t *testing.T) {
	f := newAdminFixture(t)
	root := f.addProject("col-a")
	if rec := f.do(http.MethodPost, "/v1/admin/reload-projects", "", true); rec.Code != http.StatusOK {
		t.Fatalf("initial reload: status=%d", rec.Code)
	}
	_, release, err := f.reg.BorrowService("col-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, config.DefaultInstallDirName, config.ProjectFileName),
		[]byte("schema_version: 1\ncollection_id: col-a\nindexing:\n  skip_dirs: [vendor]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := f.do(http.MethodPost, "/v1/admin/reload-projects", "", true)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Collections []string `json:"collections"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Collections) != 1 || payload.Collections[0] != "col-a" {
		t.Fatalf("payload=%+v", payload)
	}
	release()
	if rec := f.do(http.MethodPost, "/v1/admin/reload-projects", "", true); rec.Code != http.StatusOK {
		t.Fatalf("retry after release: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAdminReloadAfterShutdown: a closing registry answers 503.
func TestAdminReloadAfterShutdown(t *testing.T) {
	f := newAdminFixture(t)
	f.reg.Close()
	rec := f.do(http.MethodPost, "/v1/admin/reload-projects", "", true)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAdminReloadPerProjectMode: the route is unavailable outside daemon mode.
func TestAdminReloadPerProjectMode(t *testing.T) {
	settings := testSettings()
	settings.APIToken = "admin-token"
	srv := New(settings, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/reload-projects", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// fakeLifecycle records shutdown requests without exiting the test process.
type fakeLifecycle struct {
	mu     sync.Mutex
	calls  int
	ctxErr error
}

func (f *fakeLifecycle) RequestShutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.ctxErr = ctx.Err()
	return nil
}

func (f *fakeLifecycle) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestAdminStop: an authenticated stop returns 202 before the shutdown runs, and
// the handler never kills the process itself.
func TestAdminStop(t *testing.T) {
	f := newAdminFixture(t)
	lifecycle := &fakeLifecycle{}
	f.srv.SetDaemonLifecycle(lifecycle)

	if rec := f.do(http.MethodPost, "/v1/admin/stop", "", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer: status=%d", rec.Code)
	}
	rec := f.do(http.MethodPost, "/v1/admin/stop", "", true)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if lifecycle.Calls() != 1 {
		t.Fatalf("shutdown calls=%d", lifecycle.Calls())
	}

	// A server without a lifecycle refuses instead of pretending to stop.
	srv := NewDaemon(f.srv.settings, f.reg)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/stop", nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req)
	if rec2.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rec2.Code, rec2.Body.String())
	}
}
