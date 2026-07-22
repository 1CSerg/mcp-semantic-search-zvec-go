package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
)

func testSettings() *config.Settings {
	return &config.Settings{
		HTTPAddr: ":8080",
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible", Model: "m", Dimensions: 384},
			},
		},
	}
}

func TestHealthEndpoint(t *testing.T) {
	settings := testSettings()
	srv := New(settings, service.NewStub(settings))
	rec := httptest.NewRecorder()
	srv.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestHandlerRoutes(t *testing.T) {
	settings := testSettings()
	stub := service.NewStub(settings)
	srv := New(settings, stub)
	handler := srv.Handler()

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"health", http.MethodGet, "/health", "", http.StatusOK},
		{"ready", http.MethodGet, "/ready", "", http.StatusOK},
		{"version", http.MethodGet, "/v1/version", "", http.StatusOK},
		{"status", http.MethodGet, "/v1/status", "", http.StatusOK},
		{"workspaces", http.MethodGet, "/v1/workspaces", "", http.StatusNotImplemented},
		{"search ok", http.MethodPost, "/v1/search", `{"query":"auth"}`, http.StatusOK},
		{"search empty", http.MethodPost, "/v1/search", `{"query":"  "}`, http.StatusOK},
		{"search bad json", http.MethodPost, "/v1/search", `{`, http.StatusBadRequest},
		{"search negative limit", http.MethodPost, "/v1/search", `{"query":"auth","limit":-1}`, http.StatusBadRequest},
		{"reindex", http.MethodPost, "/v1/reindex", `{"force":true}`, http.StatusOK},
		{"reindex empty body", http.MethodPost, "/v1/reindex", "", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			if tc.method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandlerBearerAuth(t *testing.T) {
	settings := testSettings()
	settings.APIToken = "secret-token"
	srv := New(settings, service.NewStub(settings))
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health probe should stay public, status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Authorization", "bearer secret-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("lowercase bearer status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer short")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-length token status=%d", rec.Code)
	}
}

func TestDaemonVersionReusesChecker(t *testing.T) {
	t.Setenv("CHECK_UPDATE_DISABLE", "true")
	settings := testSettings()
	srv := NewDaemon(settings, nil)
	if srv.updateChecker == nil {
		t.Fatal("expected shared update checker on daemon server")
	}
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		srv.handleVersion(rec, httptest.NewRequest(http.MethodGet, "/v1/version", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("iteration %d status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}
}

type failingService struct {
	service.Service
}

func (failingService) Ready(context.Context) error {
	return errors.New("not ready")
}

func (failingService) SemanticSearch(context.Context, service.SearchRequest) (json.RawMessage, error) {
	return nil, errors.New("search failed")
}

func (failingService) GetIndexStatus(context.Context) (json.RawMessage, error) {
	return nil, errors.New("status failed")
}

func (failingService) CheckUpdate(context.Context) (json.RawMessage, error) {
	return nil, errors.New("update failed")
}

func (failingService) Reindex(context.Context, service.ReindexRequest) (json.RawMessage, error) {
	return nil, errors.New("reindex failed")
}

type indexingPartialService struct {
	service.Service
}

func (indexingPartialService) SemanticSearch(context.Context, service.SearchRequest) (json.RawMessage, error) {
	return json.RawMessage(`{"results":[{"path":"a.go","score":0.9}],"indexing":{"running":true},"message":"results may be incomplete while indexing is in progress"}`), nil
}

func TestHandlerSearchDuringIndexing(t *testing.T) {
	settings := testSettings()
	srv := New(settings, indexingPartialService{Service: service.NewStub(settings)})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/search", bytes.NewReader([]byte(`{"query":"x"}`)))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	idx, _ := payload["indexing"].(map[string]any)
	if idx == nil || idx["running"] != true {
		t.Fatalf("payload=%v", payload)
	}
	results, _ := payload["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results=%v", payload["results"])
	}
}

type embedUnreachableService struct {
	service.Service
}

func (embedUnreachableService) Ready(context.Context) error {
	return errors.New("embeddings unreachable: Get \"http://secret:8080/v1/embeddings\": connection refused")
}

func TestHandleReadySanitizesError(t *testing.T) {
	settings := testSettings()
	srv := New(settings, embedUnreachableService{Service: service.NewStub(settings)})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret:8080") {
		t.Fatalf("leaked endpoint in body=%s", body)
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "embeddings unreachable" {
		t.Fatalf("error=%q", payload["error"])
	}
}

func TestReadyPublicMessage(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New("indexing in progress"), "indexing in progress"},
		{errors.New("embeddings unreachable: http://x"), "embeddings unreachable"},
		{errors.New("boom http://secret"), "not ready"},
		{context.Canceled, "request canceled"},
		{context.DeadlineExceeded, "request canceled"},
	}
	for _, tc := range cases {
		if got := readyPublicMessage(tc.err); got != tc.want {
			t.Fatalf("readyPublicMessage(%v)=%q want %q", tc.err, got, tc.want)
		}
	}
}

func TestWriteWorkspaceErrorStatuses(t *testing.T) {
	t.Run("registry closing", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeWorkspaceError(rec, daemon.ErrRegistryClosing)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d", rec.Code)
		}
		var payload map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["error"] != "registry is closing" {
			t.Fatalf("error=%q", payload["error"])
		}
	})
}

func TestWriteErrorStatuses(t *testing.T) {
	t.Run("canceled", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeError(rec, context.Canceled)
		if rec.Code != statusClientClosedRequest {
			t.Fatalf("status=%d", rec.Code)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeError(rec, context.DeadlineExceeded)
		if rec.Code != http.StatusGatewayTimeout {
			t.Fatalf("status=%d", rec.Code)
		}
	})
	t.Run("internal", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeError(rec, errors.New("secret path /tmp/index"))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d", rec.Code)
		}
	})
}

func TestHandlerServiceErrors(t *testing.T) {
	settings := testSettings()
	srv := New(settings, failingService{Service: service.NewStub(settings)})
	handler := srv.Handler()

	t.Run("ready not ready", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d", rec.Code)
		}
	})

	t.Run("search error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/search", bytes.NewReader([]byte(`{"query":"x"}`)))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d", rec.Code)
		}
	})

	t.Run("status error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d", rec.Code)
		}
	})

	t.Run("version error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/version", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d", rec.Code)
		}
	})

	t.Run("reindex error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/reindex", bytes.NewReader([]byte(`{"force":true}`)))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d", rec.Code)
		}
	})
}

func TestRedactDaemonStatusPaths(t *testing.T) {
	raw := json.RawMessage(`{
		"workspace_root":"/secret/ws",
		"index_dir":".mcp/index",
		"config_path":"config.yaml",
		"zvec_collection_path":"zvec/col",
		"embedding_model_path":"models/x",
		"server_version":"1.0.0",
		"zvec_error":"open failed: D:\\secret\\index\\zvec\\col",
		"message":"profile load failed: C:\\Users\\alice\\proj\\config.yaml",
		"identity_mismatch_reason":"profile mismatch at /secret/ws/.mcp/config.yaml",
		"diagnostics":{"log_dir":"logs","log_file":"logs/server.log","hint":"ok"},
		"indexing":{"current_file":"src/a.go","failed_files":["b.go"],"skipped_paths":["c.go"],"state":"error","message":"indexing failed: /secret/ws/src/a.go","error":"chunk failed: D:\\secret\\ws\\src\\b.go","scan_warnings":["skip /secret/ws/ignored: permission denied"]}
	}`)
	out := redactDaemonStatusPaths(raw)
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"workspace_root", "index_dir", "config_path", "zvec_collection_path", "embedding_model_path"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("key %q not redacted", key)
		}
	}
	for _, key := range []string{"zvec_error", "message", "identity_mismatch_reason"} {
		v, ok := payload[key].(string)
		if !ok {
			t.Fatalf("key %q missing or not string", key)
		}
		if strings.Contains(v, "/secret/") || strings.Contains(v, `C:\Users\alice`) || strings.Contains(v, `D:\secret`) {
			t.Fatalf("key %q still contains path: %q", key, v)
		}
	}
	diag := payload["diagnostics"].(map[string]any)
	if _, ok := diag["log_dir"]; ok {
		t.Fatal("log_dir not redacted")
	}
	idx := payload["indexing"].(map[string]any)
	if _, ok := idx["current_file"]; ok {
		t.Fatal("current_file not redacted")
	}
	if msg, ok := idx["message"].(string); ok && strings.Contains(msg, "/secret/") {
		t.Fatalf("indexing message not sanitized: %q", msg)
	}
	if errMsg, ok := idx["error"].(string); ok {
		if strings.Contains(errMsg, "/secret/") || strings.Contains(errMsg, `D:\secret`) {
			t.Fatalf("indexing error not sanitized: %q", errMsg)
		}
	} else {
		t.Fatal("indexing.error missing or not string")
	}
	warnings := idx["scan_warnings"].([]any)
	if len(warnings) != 1 {
		t.Fatalf("scan_warnings=%v", warnings)
	}
	if w, ok := warnings[0].(string); !ok || strings.Contains(w, "/secret/") {
		t.Fatalf("scan_warnings not sanitized: %v", warnings)
	}
	if payload["server_version"] != "1.0.0" {
		t.Fatalf("server_version=%v", payload["server_version"])
	}
}

func TestRedactDaemonSearchResponse(t *testing.T) {
	raw := json.RawMessage(`{
		"query":"auth",
		"results":[{"path":"src/a.go","score":0.9}],
		"message":"results may be incomplete while indexing is in progress at /secret/ws",
		"indexing":{
			"running":true,
			"current_file":"/secret/ws/src/a.go",
			"failed_files":["/secret/b.go"],
			"skipped_paths":["/secret/c.go"],
			"message":"indexing failed: /secret/ws/src/a.go",
			"error":"chunk failed: D:\\secret\\ws\\src\\b.go",
			"scan_warnings":["warn: /secret/ws/skip"]
		},
		"performance":{"total_ms":42,"degraded":false}
	}`)
	srv := &Server{daemon: true, settings: testSettings()}
	out := srv.redactIfOpenDaemon(raw)
	body := string(out)
	if strings.Contains(body, "/secret/") || strings.Contains(body, `D:\secret`) {
		t.Fatalf("search response not redacted: %s", body)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	if msg, ok := payload["message"].(string); ok && strings.Contains(msg, "/secret/") {
		t.Fatalf("top-level message not sanitized: %q", msg)
	}
	idx := payload["indexing"].(map[string]any)
	for _, key := range []string{"current_file", "failed_files", "skipped_paths"} {
		if _, ok := idx[key]; ok {
			t.Fatalf("indexing key %q not redacted", key)
		}
	}
	if idx["running"] != true {
		t.Fatalf("running=%v", idx["running"])
	}
	results := payload["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results=%v", results)
	}
}

func TestRedactDaemonReindexProgress(t *testing.T) {
	raw := json.RawMessage(`{
		"started":false,
		"force":true,
		"message":"failed at /secret/ws/proj",
		"progress":{
			"state":"error",
			"current_file":"/secret/ws/src/a.go",
			"failed_files":["/secret/b.go"],
			"skipped_paths":["/secret/c.go"],
			"message":"indexing failed: /secret/ws/src/a.go",
			"error":"chunk failed: D:\\secret\\ws\\src\\b.go",
			"scan_warnings":["warn: /secret/ws/skip"]
		}
	}`)
	srv := &Server{daemon: true, settings: testSettings()}
	out := srv.redactIfOpenDaemon(raw)
	body := string(out)
	if strings.Contains(body, "/secret/") || strings.Contains(body, `D:\secret`) {
		t.Fatalf("reindex progress not redacted: %s", body)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	if msg, ok := payload["message"].(string); ok && strings.Contains(msg, "/secret/") {
		t.Fatalf("top-level message not sanitized: %q", msg)
	}
	prog := payload["progress"].(map[string]any)
	for _, key := range []string{"current_file", "failed_files", "skipped_paths"} {
		if _, ok := prog[key]; ok {
			t.Fatalf("progress key %q not redacted", key)
		}
	}
	warnings := prog["scan_warnings"].([]any)
	if w, ok := warnings[0].(string); !ok || strings.Contains(w, "/secret/") {
		t.Fatalf("progress scan_warnings not sanitized: %v", warnings)
	}
}

func writeDaemonWorkspaceConfig(t *testing.T, root string) {
	t.Helper()
	install := filepath.Join(root, ".mcp-semantic-search-zvec-go")
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	const cfg = `active_profile: smoke
profiles:
  smoke:
    provider: openai_compatible
    model: mock
    base_url: http://127.0.0.1:9/v1
    dimensions: 128
file_watcher:
  enabled: false
`
	if err := os.WriteFile(filepath.Join(install, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonModeWorkspaceRouting(t *testing.T) {
	root := t.TempDir()
	writeDaemonWorkspaceConfig(t, root)
	settings := testSettings()
	registry := daemon.NewRegistry(daemon.Config{
		MaxOpenWorkspaces: 2,
		Workspaces: []daemon.WorkspaceSpec{{
			ID:   "ws-a",
			Root: root,
		}},
	}, t.Context())
	defer registry.Close()
	srv := NewDaemon(settings, registry)
	handler := srv.Handler()

	t.Run("workspaces list default omits paths", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/workspaces", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var payload struct {
			Workspaces []map[string]any `json:"workspaces"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Workspaces) != 1 {
			t.Fatalf("workspaces=%v", payload.Workspaces)
		}
		ws := payload.Workspaces[0]
		if ws["id"] != "ws-a" {
			t.Fatalf("id=%v", ws["id"])
		}
		for _, key := range []string{"root", "index_dir", "config_path"} {
			if _, ok := ws[key]; ok {
				t.Fatalf("unexpected path key %q in default response: %v", key, ws)
			}
		}
	})

	t.Run("include_paths without token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/workspaces?include_paths=1", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("include_paths requires bearer when token set", func(t *testing.T) {
		authSettings := testSettings()
		authSettings.APIToken = "secret-token"
		authSrv := NewDaemon(authSettings, registry)
		authHandler := authSrv.Handler()

		rec := httptest.NewRecorder()
		authHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/workspaces?include_paths=1", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}

		req := httptest.NewRequest(http.MethodGet, "/v1/workspaces?include_paths=1", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		rec = httptest.NewRecorder()
		authHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var payload struct {
			Workspaces []map[string]any `json:"workspaces"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		ws := payload.Workspaces[0]
		if ws["root"] == "" {
			t.Fatalf("expected root in authed include_paths response: %v", ws)
		}
	})

	t.Run("status redacts paths without token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status?workspace_id=ws-a", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"workspace_root", "index_dir", "config_path", "zvec_collection_path"} {
			if _, ok := payload[key]; ok {
				t.Fatalf("unexpected path key %q in daemon status without token: %v", key, payload)
			}
		}
		if payload["server_version"] == "" {
			t.Fatalf("expected non-path fields: %v", payload)
		}
	})

	t.Run("reindex redacts paths without token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/reindex?workspace_id=ws-a", bytes.NewReader([]byte(`{"force":true}`)))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), root) {
			t.Fatalf("reindex leaked workspace root: %s", rec.Body.String())
		}
	})

	t.Run("search redacts paths without token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/search?workspace_id=ws-a", bytes.NewReader([]byte(`{"query":"auth"}`)))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), root) {
			t.Fatalf("search leaked workspace root: %s", rec.Body.String())
		}
	})

	t.Run("status missing workspace", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("status unknown workspace", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status?workspace_id=missing", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("version ok without workspace", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/version", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["installed_version"] == "" {
			t.Fatalf("missing installed_version: %v", payload)
		}
	})
}

func TestDaemonModeSearchRequiresWorkspace(t *testing.T) {
	settings := testSettings()
	registry := daemon.NewRegistry(daemon.Config{
		Workspaces: []daemon.WorkspaceSpec{{ID: "ws-a", Root: t.TempDir()}},
	}, t.Context())
	defer registry.Close()
	srv := NewDaemon(settings, registry)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/search", bytes.NewReader([]byte(`{"query":"auth"}`)))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestListenAndServeShutdown(t *testing.T) {
	settings := testSettings()
	srv := New(settings, service.NewStub(settings))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ListenAndServe: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for shutdown")
	}
}

type captureReindexService struct {
	service.Service
	force  bool
	called bool
}

func (c *captureReindexService) Reindex(_ context.Context, req service.ReindexRequest) (json.RawMessage, error) {
	c.called = true
	c.force = req.Force
	return json.RawMessage(`{"started":false}`), nil
}

func TestHandleReindexChunkedBody(t *testing.T) {
	settings := testSettings()
	cap := &captureReindexService{Service: service.NewStub(settings)}
	srv := New(settings, cap)
	body := strings.NewReader(`{"force":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/reindex", body)
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	rec := httptest.NewRecorder()
	srv.handleReindex(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !cap.called || !cap.force {
		t.Fatalf("called=%v force=%v", cap.called, cap.force)
	}
}
