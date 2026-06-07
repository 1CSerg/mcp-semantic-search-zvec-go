package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

// Server exposes REST endpoints backed by the shared service layer.
type Server struct {
	svc      service.Service
	settings *config.Settings
	mux      *http.ServeMux
}

// New creates an HTTP server with v1 routes and health probes.
func New(settings *config.Settings, svc service.Service) *Server {
	s := &Server{
		svc:      svc,
		settings: settings,
		mux:      http.NewServeMux(),
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
	s.mux.HandleFunc("GET /v1/workspaces", s.handleWorkspacesNotImplemented)
}

// Handler returns the root http.Handler with middleware.
func (s *Server) Handler() http.Handler {
	return s.withMiddleware(s.mux)
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := s.settings.APIToken; token != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+token {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		w.Header().Set("X-Server-Version", version.Version)
		next.ServeHTTP(w, r)
	})
}

// ListenAndServe starts the HTTP server until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
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

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if err := s.svc.Ready(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	raw, err := s.svc.CheckUpdate()
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
	raw, err := s.svc.SemanticSearch(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, raw)
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	raw, err := s.svc.GetIndexStatus()
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
	raw, err := s.svc.Reindex(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, raw)
}

func (s *Server) handleWorkspacesNotImplemented(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error":   "shared daemon workspace list — Phase 5",
		"message": "Use per-project mode or see docs/ARCHITECTURE.md",
	})
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

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
