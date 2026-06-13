package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
		{"search empty", http.MethodPost, "/v1/search", `{"query":"  "}`, http.StatusBadRequest},
		{"search bad json", http.MethodPost, "/v1/search", `{`, http.StatusBadRequest},
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
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth status=%d", rec.Code)
	}
}

type failingService struct {
	service.Service
}

func (failingService) Ready() error {
	return errors.New("not ready")
}

func (failingService) SemanticSearch(service.SearchRequest) (json.RawMessage, error) {
	return nil, errors.New("search failed")
}

func (failingService) GetIndexStatus() (json.RawMessage, error) {
	return nil, errors.New("status failed")
}

func (failingService) CheckUpdate() (json.RawMessage, error) {
	return nil, errors.New("update failed")
}

func (failingService) Reindex(service.ReindexRequest) (json.RawMessage, error) {
	return nil, errors.New("reindex failed")
}

type indexingPartialService struct {
	service.Service
}

func (indexingPartialService) SemanticSearch(service.SearchRequest) (json.RawMessage, error) {
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

func TestDaemonModeWorkspaceRouting(t *testing.T) {
	settings := testSettings()
	registry := daemon.NewRegistry(daemon.Config{
		MaxOpenWorkspaces: 2,
		Workspaces: []daemon.WorkspaceSpec{{
			ID:   "ws-a",
			Root: t.TempDir(),
		}},
	}, t.Context())
	defer registry.Close()
	srv := NewDaemon(settings, registry)
	handler := srv.Handler()

	t.Run("workspaces list", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/workspaces", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
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
