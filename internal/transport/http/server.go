package httptransport

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

// Server exposes REST endpoints backed by the shared service layer.
type Server struct {
	svc      service.Service
	registry *daemon.Registry
	settings *config.Settings
	mux      *http.ServeMux
	daemon   bool
}

// New creates an HTTP server with v1 routes and health probes (per-project mode).
func New(settings *config.Settings, svc service.Service) *Server {
	s := &Server{
		svc:      svc,
		settings: settings,
		mux:      http.NewServeMux(),
		daemon:   false,
	}
	s.routes()
	return s
}

// NewDaemon creates an HTTP server for shared multi-workspace daemon mode.
func NewDaemon(settings *config.Settings, registry *daemon.Registry) *Server {
	s := &Server{
		registry: registry,
		settings: settings,
		mux:      http.NewServeMux(),
		daemon:   true,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /ready", s.handleReady)
	s.mux.HandleFunc("GET /v1/version", s.handleVersion)
	s.mux.HandleFunc("POST /v1/search", s.handleSearch)
	s.mux.HandleFunc("GET /v1/status", s.handleStatus)
	s.mux.HandleFunc("POST /v1/reindex", s.handleReindex)
	s.mux.HandleFunc("GET /v1/workspaces", s.handleWorkspaces)
}

// Handler returns the root http.Handler with middleware.
func (s *Server) Handler() http.Handler {
	return s.withMiddleware(s.mux)
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := s.settings.APIToken; token != "" && !bearerAuthorized(r, token) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		w.Header().Set("X-Server-Version", version.Version)
		next.ServeHTTP(w, r)
	})
}

func bearerAuthorized(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	expected := "Bearer " + token
	return subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) == 1
}

// ListenAndServe starts the HTTP server until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("http shutdown", "err", err)
		}
	}()

	slog.Info("http server listening", "addr", addr)
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": version.Name,
		"version": version.Version,
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	svc, err := s.resolveService(r, r.URL.Query().Get("workspace_id"))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	if err := svc.Ready(r.Context()); err != nil {
		slog.Warn("ready check failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"error":  readyPublicMessage(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	raw, err := s.svc.CheckUpdate(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, raw)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req service.SearchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	workspaceID := firstNonEmpty(req.WorkspaceID, r.Header.Get("X-Workspace-ID"), r.URL.Query().Get("workspace_id"))
	svc, err := s.resolveService(r, workspaceID)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	raw, err := svc.SemanticSearch(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, raw)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := firstNonEmpty(r.Header.Get("X-Workspace-ID"), r.URL.Query().Get("workspace_id"))
	svc, err := s.resolveService(r, workspaceID)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	raw, err := svc.GetIndexStatus(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, raw)
}

func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	var req service.ReindexRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	workspaceID := firstNonEmpty(req.WorkspaceID, r.Header.Get("X-Workspace-ID"), r.URL.Query().Get("workspace_id"))
	svc, err := s.resolveService(r, workspaceID)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	raw, err := svc.Reindex(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, raw)
}

func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	if !s.daemon || s.registry == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error":   "shared daemon workspace list — per-project mode",
			"message": "Use --daemon mode or see docs/ARCHITECTURE.md",
		})
		return
	}
	includePaths := queryTruthy(r.URL.Query().Get("include_paths"))
	if includePaths && s.settings.APIToken != "" && !bearerAuthorized(r, s.settings.APIToken) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspaces": s.registry.ListWorkspaces(includePaths),
	})
}

func queryTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func (s *Server) resolveService(r *http.Request, workspaceID string) (service.Service, error) {
	if s.daemon {
		if strings.TrimSpace(workspaceID) == "" {
			return nil, errWorkspaceIDRequired
		}
		return s.registry.GetService(workspaceID)
	}
	if s.svc == nil {
		return nil, fmt.Errorf("service not configured")
	}
	return s.svc, nil
}

var errWorkspaceIDRequired = errors.New("workspace_id is required in shared daemon mode")

func writeWorkspaceError(w http.ResponseWriter, err error) {
	if errors.Is(err, errWorkspaceIDRequired) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if errors.Is(err, daemon.ErrUnknownWorkspace) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeError(w, err)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeRawJSON(w http.ResponseWriter, status int, raw json.RawMessage) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// statusClientClosedRequest mirrors nginx's non-standard 499 for a client that
// disconnected before the response completed.
const statusClientClosedRequest = 499

func writeError(w http.ResponseWriter, err error) {
	// Map cancellation/timeout to dedicated statuses; a generic 500 would be
	// misleading and the client is usually already gone.
	switch {
	case errors.Is(err, context.Canceled):
		slog.Debug("request canceled by client", "err", err)
		writeJSON(w, statusClientClosedRequest, map[string]string{"error": "request canceled"})
		return
	case errors.Is(err, context.DeadlineExceeded):
		slog.Warn("request timed out", "err", err)
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "request timed out"})
		return
	}
	// Log the detailed error server-side; return a generic message so internal
	// paths, zvec internals, and embedding endpoint details are not leaked.
	slog.Error("request failed", "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

// readyPublicMessage returns a client-safe readiness error without URLs or paths.
func readyPublicMessage(err error) string {
	if err == nil {
		return "not ready"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "request canceled"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "indexing in progress"):
		return "indexing in progress"
	case strings.Contains(msg, "index not built yet"):
		return "index not built yet"
	case strings.Contains(msg, "embedding provider not configured"):
		return "embedding provider not configured"
	case strings.Contains(msg, "embeddings unreachable"):
		return "embeddings unreachable"
	case strings.Contains(msg, "index_owner_mismatch"):
		return "index_owner_mismatch: run reindex with force=true"
	case strings.Contains(msg, "profile") && strings.Contains(msg, "not found"):
		return "embedding profile not configured"
	default:
		return "not ready"
	}
}
