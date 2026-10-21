package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemonclient"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
)

const projectProxyGlobalConfig = `active_profile: test
profiles:
  test:
    provider: openai_compatible
    model: mock
    base_url: http://127.0.0.1:9/v1
    dimensions: 8
file_watcher:
  enabled: false
`

// recordedRequest is one daemon request observed by the fake daemon.
type recordedRequest struct {
	Method        string
	Path          string
	Query         string
	HeaderID      string
	BodyWorkspace string
	Auth          string
}

// fakeDaemonAPI records every request and answers with a per-collection payload.
type fakeDaemonAPI struct {
	mu       sync.Mutex
	requests []recordedRequest
	srv      *httptest.Server
	status   int
}

func newFakeDaemonAPI(t *testing.T) *fakeDaemonAPI {
	t.Helper()
	f := &fakeDaemonAPI{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"mcp-semantic-search-zvec-go"}`))
	})
	record := func(w http.ResponseWriter, r *http.Request) {
		rec := recordedRequest{
			Method:   r.Method,
			Path:     r.URL.Path,
			Query:    r.URL.RawQuery,
			HeaderID: r.Header.Get("X-Workspace-ID"),
			Auth:     r.Header.Get("Authorization"),
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if len(body) > 0 {
			var payload struct {
				WorkspaceID string `json:"workspace_id"`
			}
			if err := json.Unmarshal(body, &payload); err == nil {
				rec.BodyWorkspace = payload.WorkspaceID
			}
		}
		f.mu.Lock()
		f.requests = append(f.requests, rec)
		status := f.status
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"fake failure"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Echo the routed collection so a test can prove which one served it.
		_, _ = w.Write([]byte(`{"workspace_id":"` + rec.HeaderID + `","chunks":[]}`))
	}
	mux.HandleFunc("/v1/search", record)
	mux.HandleFunc("/v1/status", record)
	mux.HandleFunc("/v1/reindex", record)
	mux.HandleFunc("/v1/version", record)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDaemonAPI) setStatus(code int) {
	f.mu.Lock()
	f.status = code
	f.mu.Unlock()
}

func (f *fakeDaemonAPI) seen() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// projectProxyFixture creates a user-level install with one registered project.
type projectProxyFixture struct {
	t       *testing.T
	userDir string
	root    string
	id      string
	cfgPath string
}

func newProjectProxyFixture(t *testing.T, id string) *projectProxyFixture {
	t.Helper()
	userDir := t.TempDir()
	t.Setenv("MCP_GLOBAL_DIR", userDir)
	if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), []byte(projectProxyGlobalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfgPath := config.ProjectConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "schema_version: 1\ncollection_id: " + id + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(userDir, "projects.d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	reg := "id: " + id + "\nroot: " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(reg), 0o600); err != nil {
		t.Fatal(err)
	}
	return &projectProxyFixture{t: t, userDir: userDir, root: root, id: id, cfgPath: cfgPath}
}

// TestProjectProxyBinding pins the core contract: every request the proxy sends
// carries the project's collection id in header, body and query, and an id in the
// incoming tool call cannot override the binding.
func TestProjectProxyBinding(t *testing.T) {
	f := newProjectProxyFixture(t, "col-a")
	d := newFakeDaemonAPI(t)

	svc, err := newProjectProxyService(projectProxyOptions{
		ProjectConfigPath: f.cfgPath,
		BaseURL:           d.srv.URL,
		Token:             "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SkipEnsureRunning: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// A tool call that tries to address another collection must not win.
	raw, err := svc.SemanticSearch(t.Context(), searchRequestWithWorkspace("query", "col-evil"))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.WorkspaceID != "col-a" {
		t.Fatalf("served by %q want col-a", payload.WorkspaceID)
	}

	if _, err := svc.GetIndexStatus(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reindex(t.Context(), reindexRequestWithWorkspace(true, "col-evil")); err != nil {
		t.Fatal(err)
	}

	reqs := d.seen()
	if len(reqs) != 3 {
		t.Fatalf("requests=%d want 3: %+v", len(reqs), reqs)
	}
	for _, req := range reqs {
		if req.HeaderID != "col-a" {
			t.Fatalf("%s: header workspace id=%q want col-a", req.Path, req.HeaderID)
		}
		if req.Auth != "Bearer 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
			t.Fatalf("%s: auth=%q", req.Path, req.Auth)
		}
		if req.Path == "/v1/search" || req.Path == "/v1/reindex" {
			if req.BodyWorkspace != "col-a" {
				t.Fatalf("%s: body workspace id=%q want col-a", req.Path, req.BodyWorkspace)
			}
		}
		if req.Path == "/v1/status" && !strings.Contains(req.Query, "workspace_id=col-a") {
			t.Fatalf("%s: query=%q", req.Path, req.Query)
		}
	}
}

// TestProjectProxyRejectsBadConfig: a missing, malformed or mismatched project
// config fails before any network or index access.
func TestProjectProxyRejectsBadConfig(t *testing.T) {
	f := newProjectProxyFixture(t, "col-a")
	d := newFakeDaemonAPI(t)

	cases := map[string]projectProxyOptions{
		"missing path": {SkipEnsureRunning: true},
		"nonexistent file": {
			ProjectConfigPath: filepath.Join(t.TempDir(), "nope.yaml"), SkipEnsureRunning: true},
		"malformed": func() projectProxyOptions {
			path := filepath.Join(t.TempDir(), config.ProjectFileName)
			if err := os.WriteFile(path, []byte("collection_id: [unclosed\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return projectProxyOptions{ProjectConfigPath: path, SkipEnsureRunning: true}
		}(),
		"unsafe id": func() projectProxyOptions {
			path := filepath.Join(t.TempDir(), config.ProjectFileName)
			if err := os.WriteFile(path, []byte("collection_id: ../escape\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return projectProxyOptions{ProjectConfigPath: path, SkipEnsureRunning: true}
		}(),
		"unregistered project": func() projectProxyOptions {
			// Valid project.yaml, but no projects.d registration for this root.
			root := t.TempDir()
			path := config.ProjectConfigPath(root)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("collection_id: col-orphan\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return projectProxyOptions{ProjectConfigPath: path, SkipEnsureRunning: true}
		}(),
		"registration root mismatch": func() projectProxyOptions {
			// The registration points at another root than the project config.
			otherRoot := t.TempDir()
			dir := filepath.Join(f.userDir, "projects.d")
			if err := os.WriteFile(filepath.Join(dir, "col-a.yaml"),
				[]byte("id: col-a\nroot: "+otherRoot+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return projectProxyOptions{ProjectConfigPath: f.cfgPath, SkipEnsureRunning: true}
		}(),
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			opts := tc
			opts.BaseURL = d.srv.URL
			if _, err := newProjectProxyService(opts); err == nil {
				t.Fatalf("%s: expected an error", name)
			}
		})
	}
	if reqs := d.seen(); len(reqs) != 0 {
		t.Fatalf("no request may be sent for an invalid project config: %+v", reqs)
	}
}

// TestProjectProxyNoLocalIndex: the proxy never creates a Phase1 or writes any
// project-local data/log files.
func TestProjectProxyNoLocalIndex(t *testing.T) {
	f := newProjectProxyFixture(t, "col-a")
	d := newFakeDaemonAPI(t)

	var factoryCalls int
	svc, err := newProjectProxyService(projectProxyOptions{
		ProjectConfigPath: f.cfgPath,
		BaseURL:           d.srv.URL,
		Token:             "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SkipEnsureRunning: true,
		ServiceFactory:    func() (any, error) { factoryCalls++; return nil, errors.New("must not be called") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetIndexStatus(t.Context()); err != nil {
		t.Fatal(err)
	}
	if factoryCalls != 0 {
		t.Fatalf("service factory calls=%d want 0", factoryCalls)
	}

	// Nothing may be written inside the project beyond the existing project.yaml.
	var created []string
	err = filepath.Walk(f.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(f.root, path)
		if relErr != nil {
			return relErr
		}
		if rel != filepath.Join(config.DefaultInstallDirName, config.ProjectFileName) {
			created = append(created, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 0 {
		t.Fatalf("proxy wrote files into the project: %v", created)
	}
}

// TestProjectProxyErrorMapping: daemon failures surface as tool errors without
// falling back to another collection, and cancellation is propagated.
func TestProjectProxyErrorMapping(t *testing.T) {
	f := newProjectProxyFixture(t, "col-a")
	d := newFakeDaemonAPI(t)
	svc, err := newProjectProxyService(projectProxyOptions{
		ProjectConfigPath: f.cfgPath,
		BaseURL:           d.srv.URL,
		Token:             "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SkipEnsureRunning: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]int{
		"unknown collection": http.StatusNotFound,
		"auth rejected":      http.StatusUnauthorized,
		"registry closing":   http.StatusServiceUnavailable,
	}
	for name, status := range cases {
		t.Run(name, func(t *testing.T) {
			d.setStatus(status)
			defer d.setStatus(0)
			if _, err := svc.GetIndexStatus(t.Context()); err == nil {
				t.Fatalf("%s: expected an error", name)
			}
		})
	}

	t.Run("cancellation", func(t *testing.T) {
		d.setStatus(0)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := svc.GetIndexStatus(ctx); err == nil {
			t.Fatal("expected a cancellation error")
		} else if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v want context.Canceled", err)
		}
	})
}

// TestProjectProxyUnreachableDaemon: an unreachable daemon yields an explanatory
// error naming the URL, not a silent empty result.
func TestProjectProxyUnreachableDaemon(t *testing.T) {
	f := newProjectProxyFixture(t, "col-a")
	svc, err := newProjectProxyService(projectProxyOptions{
		ProjectConfigPath: f.cfgPath,
		BaseURL:           "http://127.0.0.1:1",
		Token:             "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SkipEnsureRunning: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetIndexStatus(t.Context()); err == nil {
		t.Fatal("expected an error for an unreachable daemon")
	}
}

// TestProjectProxyEnsureRunning: without SkipEnsureRunning the proxy asks the
// client for a live daemon and uses the returned connection.
func TestProjectProxyEnsureRunning(t *testing.T) {
	f := newProjectProxyFixture(t, "col-a")
	d := newFakeDaemonAPI(t)

	var calls int
	svc, err := newProjectProxyService(projectProxyOptions{
		ProjectConfigPath: f.cfgPath,
		EnsureRunning: func(context.Context) (daemonclient.Connection, error) {
			calls++
			return daemonclient.Connection{BaseURL: d.srv.URL, Token: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("EnsureRunning calls=%d want 1", calls)
	}
	if _, err := svc.GetIndexStatus(t.Context()); err != nil {
		t.Fatal(err)
	}
	reqs := d.seen()
	if len(reqs) != 1 || reqs[0].HeaderID != "col-a" {
		t.Fatalf("requests=%+v", reqs)
	}
	if reqs[0].Auth != "Bearer abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" {
		t.Fatalf("auth=%q", reqs[0].Auth)
	}
}

// TestProjectProxyModeConflicts pins the CLI contract: --project-config is only
// valid with --stdio-proxy and cannot be combined with the manual flags.
func TestProjectProxyModeConflicts(t *testing.T) {
	cases := []struct {
		name string
		in   projectFlags
		want string
	}{
		{
			name: "project config without stdio proxy",
			in:   projectFlags{ProjectConfig: "/p/project.yaml"},
			want: "--project-config requires --stdio-proxy",
		},
		{
			name: "project config with workspace id",
			in:   projectFlags{ProjectConfig: "/p/project.yaml", StdioProxy: true, WorkspaceID: "ws"},
			want: "--project-config cannot be combined with --workspace-id",
		},
		{
			name: "project config with daemon url",
			in:   projectFlags{ProjectConfig: "/p/project.yaml", StdioProxy: true, DaemonURL: "http://127.0.0.1:8080"},
			want: "--project-config cannot be combined with --daemon-url",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProjectFlags(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
	if err := validateProjectFlags(projectFlags{ProjectConfig: "/p/project.yaml", StdioProxy: true}); err != nil {
		t.Fatalf("valid combination rejected: %v", err)
	}
}

// TestManagementFlagConflicts: two management commands must be rejected. Running
// only the first and exiting 0 would make a script believe both happened.
func TestManagementFlagConflicts(t *testing.T) {
	cases := []struct {
		name string
		in   projectFlags
	}{
		{"remove and stop", projectFlags{ProjectRemove: "/p", StopDaemon: true}},
		{"add and new-collection", projectFlags{ProjectAdd: "/p", ProjectNew: "/q"}},
		{"reload and add", projectFlags{ProjectsReload: true, ProjectAdd: "/p"}},
		{"add and remove", projectFlags{ProjectAdd: "/p", ProjectRemove: "/p"}},
		{"relocate and ensure", projectFlags{ProjectRelocate: "/p", EnsureDaemon: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProjectFlags(tc.in)
			if err == nil {
				t.Fatalf("expected a usage error for %+v", tc.in)
			}
			if !strings.Contains(err.Error(), "only one project command") {
				t.Fatalf("err=%v should explain that only one command can run", err)
			}
		})
	}

	// A single management command is still accepted.
	for _, in := range []projectFlags{
		{ProjectAdd: "/p"},
		{StopDaemon: true},
		{ProjectsReload: true},
	} {
		if err := validateProjectFlags(in); err != nil {
			t.Fatalf("single command %+v rejected: %v", in, err)
		}
	}
}

func searchRequestWithWorkspace(query, workspaceID string) service.SearchRequest {
	return service.SearchRequest{Query: query, WorkspaceID: workspaceID}
}

func reindexRequestWithWorkspace(force bool, workspaceID string) service.ReindexRequest {
	return service.ReindexRequest{Force: force, WorkspaceID: workspaceID}
}
