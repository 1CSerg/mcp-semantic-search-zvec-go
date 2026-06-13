package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/embeddings"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/watcher"
)

// Phase1 wires manifest read, optional OpenAI embed, zvec store, and Phase 2 indexer.
type Phase1 struct {
	Settings    *config.Settings
	embed       embeddings.Embedder
	zvec        zvec.Store
	zvecCfg     zvec.Config
	coordinator *indexer.Coordinator
	searchStats *SearchStats
	watcherInst *watcher.Watcher
	startupMsg  string
}

// NewPhase1 creates a Phase 1/2 service.
func NewPhase1(settings *config.Settings) (*Phase1, error) {
	p := &Phase1{
		Settings:    settings,
		searchStats: NewSearchStats(settings.App.Search),
	}

	profile, err := settings.ActiveProfile()
	if err != nil {
		p.zvecCfg = zvec.Config{
			IndexDir:      settings.IndexDir,
			WorkspaceRoot: settings.WorkspaceRoot,
			ProfileName:   settings.App.ActiveProfile,
		}
		p.zvec = zvec.New(p.zvecCfg)
		return p, nil
	}

	p.zvecCfg = zvec.Config{
		IndexDir:      settings.IndexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	p.zvec = zvec.New(p.zvecCfg)

	embed, err := embeddings.NewEmbedder(profile, settings.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	p.embed = embed
	p.coordinator = indexer.NewCoordinator(settings, profile, embed, p.zvec, p.zvecCfg)
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

func (p *Phase1) indexingProgress() indexer.Progress {
	if p.coordinator != nil {
		return p.coordinator.CurrentProgress()
	}
	store := indexer.NewProgressStore(p.Settings.IndexDir)
	pgr, err := store.Load()
	if err != nil {
		return indexer.Progress{State: indexer.StateIdle, Running: false}
	}
	return pgr
}

func (p *Phase1) isIndexingRunning() bool {
	if p.coordinator != nil {
		return p.coordinator.IsRunning()
	}
	return p.indexingProgress().Running
}

func (p *Phase1) SemanticSearch(req SearchRequest) (json.RawMessage, error) {
	start := time.Now()
	limit := req.Limit
	if limit == 0 && req.TopK != nil {
		limit = *req.TopK
	}
	if limit == 0 {
		limit = config.DefaultSearchLimit
	}

	idx := p.indexingProgress()
	if idx.Running {
		raw, mErr := marshal(map[string]any{
			"query":    req.Query,
			"results":  []any{},
			"indexing": idx.ToIndexingMap(),
			"message":  "indexing in progress",
		})
		if mErr != nil {
			return nil, mErr
		}
		return raw, ErrIndexingInProgress
	}

	if p.embed == nil {
		return marshal(map[string]any{
			"query":   req.Query,
			"results": []any{},
			"message": "embedding provider not configured or unsupported in Phase 1",
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
		if errors.Is(err, zvec.ErrCollectionMissing) {
			return marshal(map[string]any{
				"query":   req.Query,
				"results": []any{},
				"message": "index not found — run reindex (Phase 2) or seed a collection for testing",
			})
		}
		if errors.Is(err, zvec.ErrNotLinked) {
			return marshal(map[string]any{
				"query":   req.Query,
				"results": []any{},
				"message": fmt.Sprintf("vector store unavailable: %v", err),
			})
		}
		return marshal(map[string]any{
			"query":   req.Query,
			"results": []any{},
			"message": fmt.Sprintf("vector search failed: %v", err),
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
	totalMS := float64(time.Since(start).Milliseconds())
	p.searchStats.Record(totalMS)
	payload := map[string]any{
		"query":   req.Query,
		"results": results,
		"timing": map[string]any{
			"total_ms": totalMS,
		},
		"performance": p.searchStats.Performance(totalMS),
	}
	return marshal(payload)
}

func (p *Phase1) GetIndexStatus() (json.RawMessage, error) {
	files, chunks := p.manifestStats()
	profile, profileErr := p.Settings.ActiveProfile()

	collectionName := zvec.CollectionName(p.zvecCfg.WorkspaceRoot, p.zvecCfg.ProfileName, p.zvecCfg.Dimensions)
	collectionPath := zvec.CollectionPath(p.zvecCfg)

	docCount := 0
	zvecOpenOK := false
	var zvecErr string
	if err := p.zvec.Open(); err == nil {
		zvecOpenOK = true
		if n, err := p.zvec.DocCount(); err == nil {
			docCount = n
		} else {
			zvecErr = err.Error()
		}
	} else {
		zvecErr = err.Error()
	}

	idx := p.indexingProgress()
	payload := map[string]any{
		"workspace_root":          p.Settings.WorkspaceRoot,
		"index_dir":               p.Settings.IndexDir,
		"config_path":             p.Settings.ConfigPath,
		"server_version":          version.Version,
		"zvec_go_version":         version.ZvecGoVersion,
		"index_zvec_go_version":   p.indexZvecGoVersion(),
		"indexed_files":           files,
		"indexed_chunks_manifest": chunks,
		"zvec_doc_count":          docCount,
		"zvec_collection":         collectionName,
		"zvec_collection_path":    collectionPath,
		"zvec_open_ok":            zvecOpenOK,
		"index_meta_present":      zvec.IndexMetaPresent(p.Settings.IndexDir),
		"indexing":                idx.ToIndexingMap(),
		"file_watcher":            p.fileWatcherStatus(),
		"search_performance":      p.searchStats.Snapshot(),
		"diagnostics": map[string]any{
			"log_dir": p.Settings.LogsDir(),
		},
	}
	if zvecErr != "" {
		payload["zvec_error"] = zvecErr
	}
	if p.embed == nil && profileErr == nil {
		payload["message"] = "embedding client failed to initialize"
	}
	if profileErr == nil && profile.Provider == "onnx" && profile.ModelPath != "" {
		payload["embedding_model_path"] = profile.ModelPath
	}
	if profileErr != nil {
		payload["message"] = profileErr.Error()
	}
	if p.startupMsg != "" && profileErr == nil {
		payload["message"] = p.startupMsg
	}
	return marshal(payload)
}

func (p *Phase1) indexZvecGoVersion() string {
	meta, err := zvec.ReadIndexMeta(p.Settings.IndexDir)
	if err != nil {
		return ""
	}
	return meta.ZvecGoVersion
}

func (p *Phase1) Reindex(req ReindexRequest) (json.RawMessage, error) {
	if p.coordinator == nil {
		return marshal(map[string]any{
			"started": false,
			"force":   req.Force,
			"message": "embedding provider not configured for indexing",
		})
	}
	if p.coordinator.IsRunning() {
		idx := p.coordinator.CurrentProgress()
		return marshal(map[string]any{
			"started":  false,
			"force":    req.Force,
			"message":  "indexing already running",
			"progress": idx.ToIndexingMap(),
		})
	}
	pgr, err := p.coordinator.Start(req.Force)
	if err != nil {
		return marshal(map[string]any{
			"started":  false,
			"force":    req.Force,
			"message":  err.Error(),
			"progress": pgr.ToIndexingMap(),
		})
	}
	return marshal(map[string]any{
		"started":  true,
		"force":    req.Force,
		"message":  "indexing started",
		"progress": pgr.ToIndexingMap(),
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
	if p.isIndexingRunning() {
		return fmt.Errorf("indexing in progress")
	}
	profile, err := p.Settings.ActiveProfile()
	if err != nil {
		return err
	}
	if p.embed == nil {
		return fmt.Errorf("embedding provider not configured")
	}
	if !zvec.IndexMetaPresent(p.Settings.IndexDir) {
		return fmt.Errorf("index not built yet")
	}
	if err := zvec.ValidateIndexMeta(p.Settings.IndexDir, p.Settings.WorkspaceID, p.Settings.App.ActiveProfile, profile.Dimensions); err != nil {
		return err
	}
	if err := p.embed.HealthCheck(context.Background()); err != nil {
		return fmt.Errorf("embeddings unreachable: %w", err)
	}
	if err := p.zvec.Open(); err != nil {
		if errors.Is(err, zvec.ErrCollectionMissing) {
			return fmt.Errorf("index not built yet")
		}
		return err
	}
	return nil
}

// StartFileWatcher runs the background file watcher until ctx is cancelled.
func (p *Phase1) StartFileWatcher(ctx context.Context) {
	if p.coordinator == nil {
		return
	}
	w, err := watcher.New(p.Settings, p.coordinator)
	if err != nil {
		slog.Warn("file watcher init failed", "err", err)
		return
	}
	if w == nil {
		return
	}
	p.watcherInst = w
	go w.Start(ctx)
}

// PrepareStartup runs zvec-go migration if needed and optional background indexing on start.
func (p *Phase1) PrepareStartup() {
	if p.coordinator == nil {
		return
	}
	if p.runZvecGoMigrationIfNeeded() {
		return
	}
	p.StartAutoIndex()
}

func (p *Phase1) runZvecGoMigrationIfNeeded() bool {
	need, meta, err := zvec.NeedsZvecGoMigration(p.Settings.IndexDir, version.ZvecGoVersion)
	if err != nil {
		slog.Warn("zvec-go migration check failed", "err", err)
		return false
	}
	if !need {
		return false
	}

	from := ""
	if meta != nil {
		from = meta.ZvecGoVersion
	}
	slog.Info("zvec-go version changed; resetting index", "from", from, "to", version.ZvecGoVersion)

	if err := zvec.ResetIndexForZvecMigration(p.Settings.IndexDir, meta, p.zvec, version.ZvecGoVersion); err != nil {
		slog.Warn("zvec-go migration reset failed", "err", err)
		return false
	}

	if p.Settings.AutoIndexOnStart {
		if _, err := p.coordinator.Start(true); err != nil {
			slog.Warn("zvec-go migration reindex failed to start", "err", err)
		}
		return true
	}

	p.startupMsg = "zvec-go updated — run reindex to rebuild the index"
	slog.Info(p.startupMsg)
	return true
}

// StartAutoIndex triggers background indexing when AUTO_INDEX_ON_START is enabled.
func (p *Phase1) StartAutoIndex() {
	if p.coordinator == nil || !p.Settings.AutoIndexOnStart {
		return
	}
	_, _ = p.coordinator.Start(false)
}

// Close releases workspace resources (zvec collection handle).
func (p *Phase1) Close() error {
	if p.zvec != nil {
		return p.zvec.Close()
	}
	return nil
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
