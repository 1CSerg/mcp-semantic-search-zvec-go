package service

import (
	"encoding/json"
	"fmt"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

// ErrNotImplemented indicates bootstrap stub; real logic arrives in Phase 1+.
var ErrNotImplemented = fmt.Errorf("not implemented yet (see docs/ROADMAP.md)")

// SearchRequest is shared by MCP and HTTP.
type SearchRequest struct {
	Query    string  `json:"query"`
	Limit    int     `json:"limit,omitempty"`
	PathGlob *string `json:"path_glob,omitempty"`
	TopK     *int    `json:"top_k,omitempty"` // deprecated alias
}

// ReindexRequest triggers background indexing.
type ReindexRequest struct {
	Force bool `json:"force,omitempty"`
}

// Service is the core semantic search API used by MCP and HTTP transports.
type Service interface {
	SemanticSearch(req SearchRequest) (json.RawMessage, error)
	GetIndexStatus() (json.RawMessage, error)
	Reindex(req ReindexRequest) (json.RawMessage, error)
	CheckUpdate() (json.RawMessage, error)
	Ready() error
}

// Stub implements Service with placeholder responses for Phase 0 bootstrap.
type Stub struct {
	Settings *config.Settings
}

// NewStub creates a bootstrap service.
func NewStub(settings *config.Settings) *Stub {
	return &Stub{Settings: settings}
}

func (s *Stub) SemanticSearch(req SearchRequest) (json.RawMessage, error) {
	limit := req.Limit
	if limit == 0 && req.TopK != nil {
		limit = *req.TopK
	}
	if limit == 0 {
		limit = config.DefaultSearchLimit
	}
	payload := map[string]any{
		"query":   req.Query,
		"results": []any{},
		"message": ErrNotImplemented.Error(),
		"indexing": map[string]any{
			"state":   "idle",
			"running": false,
		},
		"limit": limit,
	}
	return marshal(payload)
}

func (s *Stub) GetIndexStatus() (json.RawMessage, error) {
	profile, _ := s.Settings.ActiveProfile()
	payload := map[string]any{
		"workspace_root":          s.Settings.WorkspaceRoot,
		"index_dir":               s.Settings.IndexDir,
		"config_path":             s.Settings.ConfigPath,
		"embedding_profile":       s.Settings.App.ActiveProfile,
		"embedding_provider":      profile.Provider,
		"embedding_model":         profile.Model,
		"embedding_dimensions":    profile.Dimensions,
		"server_version":          version.Version,
		"indexed_files":           0,
		"indexed_chunks_manifest": 0,
		"zvec_doc_count":          0,
		"bootstrap":               true,
		"message":                 ErrNotImplemented.Error(),
		"indexing": map[string]any{
			"state":   "idle",
			"running": false,
			"message": "Phase 0 bootstrap — indexer not wired",
		},
		"file_watcher": map[string]any{
			"enabled_in_config": s.Settings.App.FileWatcher.Enabled,
			"running":           false,
			"paused":            false,
		},
		"search_performance": map[string]any{
			"samples": 0,
		},
		"diagnostics": map[string]any{
			"log_dir": s.Settings.LogsDir(),
		},
	}
	return marshal(payload)
}

func (s *Stub) Reindex(req ReindexRequest) (json.RawMessage, error) {
	payload := map[string]any{
		"started": false,
		"force":   req.Force,
		"message": ErrNotImplemented.Error(),
		"progress": map[string]any{
			"state":   "idle",
			"running": false,
		},
	}
	return marshal(payload)
}

func (s *Stub) CheckUpdate() (json.RawMessage, error) {
	payload := map[string]any{
		"installed_version": version.Version,
		"latest_version":    version.Version,
		"update_available":  false,
		"github_repo":       s.Settings.GitHubRepo,
		"message":           "check_update against GitHub releases — Phase 2",
	}
	return marshal(payload)
}

func (s *Stub) Ready() error {
	return nil
}

func marshal(v any) (json.RawMessage, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}
