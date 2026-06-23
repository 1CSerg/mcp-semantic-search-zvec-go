package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

// ErrNotImplemented indicates the default stub build (no zvec tag).
var ErrNotImplemented = fmt.Errorf("not implemented in stub build; use -tags zvec,onnx")

// ErrInvalidSearchLimit is returned when search limit is negative.
var ErrInvalidSearchLimit = errors.New("limit must be non-negative")

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
	StartLine     int64   `json:"start_line"`
	EndLine       int64   `json:"end_line"`
	Path          string  `json:"path"`
	Score         float64 `json:"score"`
	Snippet       string  `json:"snippet"`
	SymbolName    string  `json:"symbol_name"`
	SymbolKind    string  `json:"symbol_kind"`
	ParentScope   string  `json:"parent_scope"`
	ChunkStrategy string  `json:"chunk_strategy"`
	ChunkType     string  `json:"chunk_type,omitempty"`
}

// ReindexRequest triggers background indexing.
type ReindexRequest struct {
	Force       bool   `json:"force,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// Service is the core semantic search API used by MCP and HTTP transports.
type Service interface {
	SemanticSearch(ctx context.Context, req SearchRequest) (json.RawMessage, error)
	GetIndexStatus(ctx context.Context) (json.RawMessage, error)
	Reindex(ctx context.Context, req ReindexRequest) (json.RawMessage, error)
	CheckUpdate(ctx context.Context) (json.RawMessage, error)
	Ready(ctx context.Context) error
}

// Stub implements Service with placeholder responses for the default stub build (!zvec tag).
type Stub struct {
	Settings *config.Settings
}

// NewStub creates a bootstrap service.
func NewStub(settings *config.Settings) *Stub {
	return &Stub{Settings: settings}
}

func (s *Stub) SemanticSearch(ctx context.Context, req SearchRequest) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit, err := normalizeSearchLimit(req.Limit, req.TopK)
	if err != nil {
		return nil, err
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

func (s *Stub) GetIndexStatus(ctx context.Context) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := s.Settings.WorkspaceRoot
	payload := map[string]any{
		"workspace_root":          root,
		"index_dir":               statusRelativePath(root, s.Settings.IndexDir),
		"config_path":             statusRelativePath(root, s.Settings.ConfigPath),
		"server_version":          version.Version,
		"chunking_strategy":       s.Settings.App.Indexing.Chunking.Strategy,
		"chunking_version":        s.Settings.App.Indexing.Chunking.Version,
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

func (s *Stub) Reindex(ctx context.Context, req ReindexRequest) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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

func (s *Stub) CheckUpdate(ctx context.Context) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"installed_version": version.Version,
		"latest_version":    version.Version,
		"update_available":  false,
		"github_repo":       s.Settings.GitHubRepo,
		"message":           "check_update not implemented in stub build",
	}
	return marshal(payload)
}

func (s *Stub) Ready(ctx context.Context) error {
	return ctx.Err()
}

func marshal(v any) (json.RawMessage, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}
