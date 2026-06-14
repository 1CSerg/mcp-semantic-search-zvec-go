package service

import (
	"encoding/json"
	"fmt"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

// ErrNotImplemented indicates the default stub build (no zvec tag).
var ErrNotImplemented = fmt.Errorf("not implemented in stub build; use -tags zvec,onnx")

// SearchRequest is shared by MCP and HTTP.
type SearchRequest struct {
	Query       string  `json:"query"`
	Limit       int     `json:"limit,omitempty"`
	PathGlob    *string `json:"path_glob,omitempty"`
	TopK        *int    `json:"top_k,omitempty"` // deprecated alias
	WorkspaceID string  `json:"workspace_id,omitempty"`
}

// SearchResultItem is one ranked chunk in semantic_search results.
type SearchResultItem struct {
	StartLine int64   `json:"start_line"`
	EndLine   int64   `json:"end_line"`
	Path      string  `json:"path"`
	Score     float64 `json:"score"`
	Snippet   string  `json:"snippet"`
}

// ReindexRequest triggers background indexing.
type ReindexRequest struct {
	Force       bool   `json:"force,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// Service is the core semantic search API used by MCP and HTTP transports.
type Service interface {
	SemanticSearch(req SearchRequest) (json.RawMessage, error)
	GetIndexStatus() (json.RawMessage, error)
	Reindex(req ReindexRequest) (json.RawMessage, error)
	CheckUpdate() (json.RawMessage, error)
	Ready() error
}

// Stub implements Service with placeholder responses for the default stub build (!zvec tag).
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
	root := s.Settings.WorkspaceRoot
	payload := map[string]any{
		"workspace_root":          root,
		"index_dir":               statusRelativePath(root, s.Settings.IndexDir),
		"config_path":             statusRelativePath(root, s.Settings.ConfigPath),
		"server_version":          version.Version,
		"indexed_files":           0,
		"indexed_chunks_manifest": 0,
		"zvec_doc_count":          0,
		"bootstrap":               true,
		"message":                 ErrNotImplemented.Error(),
		"indexing": map[string]any{
			"state":   "idle",
			"running": false,
			"message": "indexer not available in stub build",
		},
		"file_watcher": map[string]any{
			"enabled_in_config": s.Settings.App.FileWatcher.Enabled,
			"running":           false,
			"paused":            false,
		},
		"search_performance": map[string]any{
			"samples": 0,
		},
		"diagnostics": indexStatusDiagnostics(s.Settings),
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
		"message":           "check_update not implemented in stub build",
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
