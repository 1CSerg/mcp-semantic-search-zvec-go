package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/embeddings/openai"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

// Phase1 wires manifest read, optional OpenAI embed, and zvec stub/real store.
type Phase1 struct {
	Settings *config.Settings
	embed    *openai.Client
	zvec     zvec.Store
}

// NewPhase1 creates a Phase 1 service (search requires zvec-go integration).
func NewPhase1(settings *config.Settings) (*Phase1, error) {
	p := &Phase1{
		Settings: settings,
		zvec:     zvec.NewStub(),
	}
	profile, err := settings.ActiveProfile()
	if err != nil {
		return p, nil
	}
	if profile.Provider == "openai_compatible" {
		c, err := openai.NewClient(profile)
		if err != nil {
			return nil, err
		}
		p.embed = c
	}
	return p, nil
}

func (p *Phase1) manifestStats() (files, chunks int) {
	dbPath := filepath.Join(p.Settings.IndexDir, "manifest.db")
	if _, err := os.Stat(dbPath); err != nil {
		return 0, 0
	}
	store, err := manifest.Open(dbPath)
	if err != nil {
		return 0, 0
	}
	defer store.Close()
	f, c, err := store.Stats()
	if err != nil {
		return 0, 0
	}
	return f, c
}

func (p *Phase1) SemanticSearch(req SearchRequest) (json.RawMessage, error) {
	limit := req.Limit
	if limit == 0 && req.TopK != nil {
		limit = *req.TopK
	}
	if limit == 0 {
		limit = config.DefaultSearchLimit
	}

	if p.embed == nil {
		return marshal(map[string]any{
			"query":   req.Query,
			"results": []any{},
			"message": "embedding provider not configured or unsupported in Phase 1 bootstrap",
		})
	}

	ctx := context.Background()
	vector, err := p.embed.EmbedQuery(ctx, req.Query)
	if err != nil {
		return marshal(map[string]any{
			"query":   req.Query,
			"results": []any{},
			"message": fmt.Sprintf("embedding failed: %v", err),
		})
	}

	hits, err := p.zvec.Search(vector, limit, derefString(req.PathGlob))
	if err != nil {
		return marshal(map[string]any{
			"query":   req.Query,
			"results": []any{},
			"message": fmt.Sprintf("vector search pending zvec-go integration: %v", err),
			"indexing": map[string]any{
				"state": "idle",
			},
		})
	}

	results := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		results = append(results, map[string]any{
			"path":       h.Path,
			"start_line": h.StartLine,
			"end_line":   h.EndLine,
			"score":      h.Score,
			"snippet":    h.Snippet,
		})
	}
	return marshal(map[string]any{
		"query":   req.Query,
		"results": results,
	})
}

func (p *Phase1) GetIndexStatus() (json.RawMessage, error) {
	files, chunks := p.manifestStats()
	profile, _ := p.Settings.ActiveProfile()
	docCount := 0
	if err := p.zvec.Open(); err == nil {
		if n, err := p.zvec.DocCount(); err == nil {
			docCount = n
		}
	}
	payload := map[string]any{
		"workspace_root":          p.Settings.WorkspaceRoot,
		"index_dir":               p.Settings.IndexDir,
		"config_path":             p.Settings.ConfigPath,
		"embedding_profile":       p.Settings.App.ActiveProfile,
		"embedding_provider":      profile.Provider,
		"embedding_model":         profile.Model,
		"embedding_dimensions":    profile.Dimensions,
		"server_version":          version.Version,
		"indexed_files":           files,
		"indexed_chunks_manifest": chunks,
		"zvec_doc_count":          docCount,
		"phase":                   "1-bootstrap",
		"indexing": map[string]any{
			"state":   "idle",
			"running": false,
		},
		"file_watcher": map[string]any{
			"enabled_in_config": p.Settings.App.FileWatcher.Enabled,
			"running":           false,
		},
		"search_performance": map[string]any{"samples": 0},
		"diagnostics": map[string]any{
			"log_dir": p.Settings.LogsDir(),
		},
	}
	if p.embed == nil && profile.Provider == "openai_compatible" {
		payload["message"] = "openai_compatible client failed to initialize"
	}
	return marshal(payload)
}

func (p *Phase1) Reindex(req ReindexRequest) (json.RawMessage, error) {
	return marshal(map[string]any{
		"started": false,
		"force":   req.Force,
		"message": "indexer write path — Phase 2",
	})
}

func (p *Phase1) CheckUpdate() (json.RawMessage, error) {
	return marshal(map[string]any{
		"installed_version": version.Version,
		"latest_version":    version.Version,
		"update_available":  false,
		"github_repo":       p.Settings.GitHubRepo,
	})
}

func (p *Phase1) Ready() error {
	return nil
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
