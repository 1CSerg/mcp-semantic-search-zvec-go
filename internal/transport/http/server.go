package httptransport

import (
	"context"
	"crypto/sha256"
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
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/redact"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/update"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

// Server exposes REST endpoints backed by the shared service layer.
type Server struct {
	svc           service.Service
	registry      *daemon.Registry
	settings      *config.Settings
	mux           *http.ServeMux
	daemon        bool
	updateChecker *update.Checker
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
	repo := ""
	if settings != nil {
		repo = settings.GitHubRepo
	}
	s := &Server{
		registry:      registry,
		settings:      settings,
		mux:           http.NewServeMux(),
		daemon:        true,
		updateChecker: update.NewChecker(repo),
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
		// Probes stay unauthenticated so orchestrators and test harness can wait for listen.
		if r.URL.Path != "/health" {
			if token := s.settings.APIToken; token != "" && !bearerAuthorized(r, token) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		w.Header().Set("X-Server-Version", version.Version)
		next.ServeHTTP(w, r)
	})
}

func bearerAuthorized(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) < 7 || !strings.EqualFold(auth[:7], "Bearer ") {
		return false
	}
	got := strings.TrimSpace(auth[7:])
	gotHash := sha256.Sum256([]byte(got))
	wantHash := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1
}

// ListenAndServe starts the HTTP server until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	writeTimeout := 120 * time.Second
	if s.settings != nil {
		if profile, err := s.settings.ActiveProfile(); err == nil {
			writeTimeout = config.EmbedHTTPBudget(profile)
		}
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       120 * time.Second,
	}

	// served signals the shutdown goroutine that ListenAndServe has returned
	// (whether on ctx cancellation or on its own, e.g. a bind error) so the
	// goroutine does not block on <-ctx.Done() forever after this function
	// returns — which would leak the goroutine if the server failed to start.
	served := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-served:
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("http shutdown", "err", err)
		}
	}()

	slog.Info("http server listening", "addr", addr)
	err := srv.ListenAndServe()
	close(served)
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
	svc, release, err := s.borrowService(r, r.URL.Query().Get("workspace_id"))
	if release != nil {
		defer release()
	}
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
	if s.daemon {
		info := s.updateChecker.Check(r.Context(), version.Version)
		writeJSON(w, http.StatusOK, info)
		return
	}
	raw, err := s.svc.CheckUpdate(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, raw)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req service.SearchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	workspaceID := firstNonEmpty(req.WorkspaceID, r.Header.Get("X-Workspace-ID"), r.URL.Query().Get("workspace_id"))
	svc, release, err := s.borrowService(r, workspaceID)
	if release != nil {
		defer release()
	}
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	raw, err := svc.SemanticSearch(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	raw = s.redactIfOpenDaemon(raw)
	writeRawJSON(w, http.StatusOK, raw)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := firstNonEmpty(r.Header.Get("X-Workspace-ID"), r.URL.Query().Get("workspace_id"))
	svc, release, err := s.borrowService(r, workspaceID)
	if release != nil {
		defer release()
	}
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	raw, err := svc.GetIndexStatus(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	raw = s.redactIfOpenDaemon(raw)
	writeRawJSON(w, http.StatusOK, raw)
}

func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	var req service.ReindexRequest
	if r.Body != nil && r.Body != http.NoBody {
		if err := decodeJSON(w, r, &req); err != nil {
			if errors.Is(err, io.EOF) {
				// Empty body is valid for incremental reindex.
			} else {
				writeDecodeError(w, err)
				return
			}
		}
	}
	workspaceID := firstNonEmpty(req.WorkspaceID, r.Header.Get("X-Workspace-ID"), r.URL.Query().Get("workspace_id"))
	svc, release, err := s.borrowService(r, workspaceID)
	if release != nil {
		defer release()
	}
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	raw, err := svc.Reindex(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	raw = s.redactIfOpenDaemon(raw)
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
	if includePaths && (s.settings.APIToken == "" || !bearerAuthorized(r, s.settings.APIToken)) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspaces": s.registry.ListWorkspaces(includePaths),
	})
}

func (s *Server) redactIfOpenDaemon(raw json.RawMessage) json.RawMessage {
	if s.daemon && s.settings != nil && s.settings.APIToken == "" {
		return redactDaemonStatusPaths(raw)
	}
	return raw
}

func redactDaemonStatusPaths(raw json.RawMessage) json.RawMessage {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw
	}
	for _, key := range []string{
		"workspace_root",
		"index_dir",
		"config_path",
		"zvec_collection_path",
		"embedding_model_path",
	} {
		delete(payload, key)
	}
	for _, key := range []string{"zvec_error", "message", "identity_mismatch_reason"} {
		if v, ok := payload[key].(string); ok && v != "" {
			payload[key] = redact.SanitizeErrorText(v)
		}
	}
	if fw, ok := payload["file_watcher"].(map[string]any); ok {
		if v, ok := fw["last_error"].(string); ok && v != "" {
			fw["last_error"] = redact.SanitizeErrorText(v)
		}
	}
	if diag, ok := payload["diagnostics"].(map[string]any); ok {
		delete(diag, "log_dir")
		delete(diag, "log_file")
	}
	if idx, ok := payload["indexing"].(map[string]any); ok {
		redactDaemonIndexingMap(idx)
	}
	if prog, ok := payload["progress"].(map[string]any); ok {
		redactDaemonIndexingMap(prog)
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return out
}

func redactDaemonIndexingMap(idx map[string]any) {
	delete(idx, "current_file")
	delete(idx, "failed_files")
	delete(idx, "skipped_paths")
	if msg, ok := idx["message"].(string); ok && msg != "" {
		idx["message"] = redact.SanitizeErrorText(msg)
	}
	if errMsg, ok := idx["error"].(string); ok && errMsg != "" {
		idx["error"] = redact.SanitizeErrorText(errMsg)
	}
	if warnings, ok := idx["scan_warnings"].([]any); ok {
		for i, w := range warnings {
			if s, ok := w.(string); ok {
				warnings[i] = redact.SanitizeErrorText(s)
			}
		}
	}
}

func queryTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func (s *Server) borrowService(_ *http.Request, workspaceID string) (service.Service, func(), error) {
	if s.daemon {
		if strings.TrimSpace(workspaceID) == "" {
			return nil, nil, errWorkspaceIDRequired
		}
		return s.registry.BorrowService(workspaceID)
	}
	if s.svc == nil {
		return nil, nil, fmt.Errorf("service not configured")
	}
	return s.svc, func() {}, nil
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
	if errors.Is(err, daemon.ErrRegistryClosing) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
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

func writeDecodeError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	defer func() { _ = r.Body.Close() }()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		slog.Warn("write JSON response failed", "err", err)
	}
}

func writeRawJSON(w http.ResponseWriter, status int, raw json.RawMessage) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(raw); err != nil {
		slog.Warn("write raw JSON response failed", "err", err)
	}
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
	case errors.Is(err, service.ErrInvalidSearchLimit):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	case errors.Is(err, service.ErrInternalSearch):
		slog.Error("request failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
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
