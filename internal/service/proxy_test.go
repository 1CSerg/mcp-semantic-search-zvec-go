package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPProxySemanticSearch(t *testing.T) {
	const workspaceID = "ws-a"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if got := r.Header.Get("X-Workspace-ID"); got != workspaceID {
			t.Fatalf("header workspace=%q", got)
		}
		var req SearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.WorkspaceID != workspaceID {
			t.Fatalf("body workspace=%q", req.WorkspaceID)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"auth","results":[{"path":"a.go","score":0.9}]}`))
	}))
	defer srv.Close()

	proxy := NewHTTPProxy(srv.URL, workspaceID, "token")
	raw, err := proxy.SemanticSearch(context.Background(), SearchRequest{Query: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	results, ok := payload["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results=%v", payload["results"])
	}
}

func TestHTTPProxyStatusQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.URL.Query().Get("workspace_id") != "ws-b" {
			t.Fatalf("query=%v", r.URL.Query())
		}
		_, _ = w.Write([]byte(`{"workspace_root":"/tmp"}`))
	}))
	defer srv.Close()

	proxy := NewHTTPProxy(srv.URL, "ws-b", "")
	raw, err := proxy.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("empty response")
	}
}

func TestHTTPProxyIndexingConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"results":[],"indexing":{"running":true}}`))
	}))
	defer srv.Close()

	proxy := NewHTTPProxy(srv.URL, "ws", "")
	_, err := proxy.SemanticSearch(context.Background(), SearchRequest{Query: "x"})
	if err != ErrIndexingInProgress {
		t.Fatalf("err=%v", err)
	}
}

func TestHTTPProxyReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("workspace_id") != "ws" {
			t.Fatalf("query=%v", r.URL.Query())
		}
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}))
	defer srv.Close()

	proxy := NewHTTPProxy(srv.URL, "ws", "")
	if err := proxy.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPProxyReadyNotReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not_ready","error":"index not built yet"}`))
	}))
	defer srv.Close()

	proxy := NewHTTPProxy(srv.URL, "ws", "")
	if err := proxy.Ready(context.Background()); err == nil || err.Error() != "index not built yet" {
		t.Fatalf("err=%v", err)
	}
}

func TestHTTPProxyReindexAndVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/reindex":
			_, _ = w.Write([]byte(`{"started":true}`))
		case "/v1/version":
			_, _ = w.Write([]byte(`{"installed_version":"1.0.0"}`))
		default:
			t.Fatalf("path=%s", r.URL.Path)
		}
	}))
	defer srv.Close()

	proxy := NewHTTPProxy(srv.URL, "ws", "tok")
	raw, err := proxy.Reindex(context.Background(), ReindexRequest{Force: true})
	if err != nil || len(raw) == 0 {
		t.Fatalf("reindex err=%v raw=%s", err, raw)
	}
	raw, err = proxy.CheckUpdate(context.Background())
	if err != nil || len(raw) == 0 {
		t.Fatalf("version err=%v raw=%s", err, raw)
	}
}

func TestHTTPProxyBearerToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Fatalf("auth=%q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"workspace_root":"/tmp"}`))
	}))
	defer srv.Close()

	proxy := NewHTTPProxy(srv.URL, "ws", "secret-token")
	if _, err := proxy.GetIndexStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPProxyHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"unknown workspace"}`))
	}))
	defer srv.Close()

	proxy := NewHTTPProxy(srv.URL, "ws", "")
	_, err := proxy.GetIndexStatus(context.Background())
	if err == nil || err.Error() != "unknown workspace" {
		t.Fatalf("err=%v", err)
	}
}

func TestHTTPProxyRespectsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	proxy := NewHTTPProxy(srv.URL, "ws", "")
	_, err := proxy.GetIndexStatus(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
