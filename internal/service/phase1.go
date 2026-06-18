package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/embeddings"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lifecycle"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/update"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/watcher"
)

var errZvecRecoverySkipped = errors.New("zvec recovery skipped: indexing active")

// Phase1 wires manifest read, embeddings, zvec store, and the indexer.
type Phase1 struct {
	Settings     *config.Settings
	embed        embeddings.Embedder
	zvec         zvec.Store
	zvecCfg      zvec.Config
	coordinator  *indexer.Coordinator
	searchStats  *SearchStats
	watcherInst  *watcher.Watcher
	startupMsg   string
	lifecycleCtx context.Context
	startupMu    sync.RWMutex

	updateChecker *update.Checker

	zvecLockWarnMu   sync.Mutex
	lastZvecLockWarn time.Time
	shutdownOnce     sync.Once
	searchWG         sync.WaitGroup
}

// NewPhase1 creates the production service (zvec + indexer).
func NewPhase1(settings *config.Settings) (*Phase1, error) {
	p := &Phase1{
		Settings:    settings,
		searchStats: NewSearchStats(settings.App.Search),
	}

	profile, err := settings.ActiveProfile()
	if err != nil {
		return nil, err
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
	p.updateChecker = update.NewChecker(settings.GitHubRepo)
	return p, nil
}

// SetLifecycleContext binds process/workspace shutdown context for background indexing.
func (p *Phase1) SetLifecycleContext(ctx context.Context) {
	p.lifecycleCtx = ctx
	if p.coordinator != nil {
		p.coordinator.SetLifecycleContext(ctx)
	}
}

func (p *Phase1) manifestStats() (files, chunks int) {
	dbPath := filepath.Join(p.Settings.IndexDir, "manifest.db")
	if _, err := os.Stat(dbPath); err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("manifest stat failed", "path", dbPath, "err", err)
		}
		return 0, 0
	}
	store, err := manifest.Open(dbPath)
	if err != nil {
		slog.Warn("manifest open for stats failed", "path", dbPath, "err", err)
		return 0, 0
	}
	defer store.Close()
	f, c, err := store.Stats()
	if err != nil {
		slog.Warn("manifest stats failed", "path", dbPath, "err", err)
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

func (p *Phase1) SemanticSearch(ctx context.Context, req SearchRequest) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.searchWG.Add(1)
	defer p.searchWG.Done()

	start := time.Now()
	limit, err := normalizeSearchLimit(req.Limit, req.TopK)
	if err != nil {
		return nil, err
	}

	idx := p.indexingProgress()

	if p.embed == nil {
		return marshal(map[string]any{
			"query":   req.Query,
			"results": []any{},
			"message": "embedding provider not configured or unsupported",
		})
	}

	vector, err := p.embed.EmbedQuery(ctx, req.Query)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return marshal(map[string]any{
			"query":   req.Query,
			"results": []any{},
			"message": fmt.Sprintf("embedding failed: %v", err),
		})
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	hits, err := p.zvecSearchWithContext(ctx, vector, limit, derefString(req.PathGlob))
	if err != nil && lifecycle.IsZvecLockError(err) {
		recErr := p.recoverZvecLock()
		if recErr == nil {
			hits, err = p.zvecSearchWithContext(ctx, vector, limit, derefString(req.PathGlob))
		} else if !errors.Is(recErr, errZvecRecoverySkipped) {
			err = fmt.Errorf("zvec lock recovery failed: %w (search: %w)", recErr, err)
		}
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if errors.Is(err, zvec.ErrCollectionMissing) {
			return marshal(map[string]any{
				"query":   req.Query,
				"results": []any{},
				"message": "index not found — run reindex or seed a collection for testing",
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

	results := make([]SearchResultItem, 0, len(hits))
	for _, h := range hits {
		results = append(results, SearchResultItem{
			StartLine: h.StartLine,
			EndLine:   h.EndLine,
			Path:      h.Path,
			Score:     h.Score,
			Snippet:   h.Snippet,
		})
	}
	totalMS := float64(time.Since(start).Milliseconds())
	p.searchStats.Record(totalMS)
	payload := map[string]any{
		"query":       req.Query,
		"results":     results,
		"performance": p.searchStats.Performance(totalMS),
	}
	if idx.Running {
		payload["indexing"] = idx.ToIndexingMap()
		if _, hasMsg := payload["message"]; !hasMsg {
			payload["message"] = "results may be incomplete while indexing is in progress"
		}
	}
	return marshal(payload)
}

func (p *Phase1) GetIndexStatus(ctx context.Context) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files, chunks := p.manifestStats()
	profile, profileErr := p.Settings.ActiveProfile()

	collectionName := zvec.CollectionName(p.zvecCfg.WorkspaceRoot, p.zvecCfg.ProfileName, p.zvecCfg.Dimensions)
	collectionPath := zvec.CollectionPath(p.zvecCfg)

	docCount := 0
	zvecOpenOK := false
	var zvecErr string
	if err := p.openZvecWithRecovery(); err == nil {
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
	root := p.Settings.WorkspaceRoot
	diag := indexStatusDiagnostics(p.Settings)
	if !zvecOpenOK && lifecycle.IsZvecLockError(errors.New(zvecErr)) {
		diag["duplicate_stdio_suspected"] = true
		diag["hint"] = "Restart Cursor or kill extra mcp-semantic-search-zvec-go processes for this workspace"
	}
	filesFailed := 0
	if v, ok := idx.ToIndexingMap()["files_failed"]; ok {
		switch n := v.(type) {
		case int:
			filesFailed = n
		case float64:
			filesFailed = int(n)
		}
	}
	profileDims := 0
	if profileErr == nil {
		profileDims = profile.Dimensions
	}
	identityMismatch, _, identityErr := zvec.IndexIdentityMismatch(
		p.Settings.IndexDir,
		p.Settings.WorkspaceID,
		p.Settings.App.ActiveProfile,
		profileDims,
	)
	enrichIndexStatusDiagnostics(diag, p.Settings, filesFailed, docCount, chunks, zvecOpenOK, len(idx.SkippedPaths), identityMismatch)
	payload := map[string]any{
		"workspace_root":          root,
		"index_dir":               statusRelativePath(root, p.Settings.IndexDir),
		"config_path":             statusRelativePath(root, p.Settings.ConfigPath),
		"server_version":          version.Version,
		"zvec_go_version":         version.ZvecGoVersion,
		"index_zvec_go_version":   p.indexZvecGoVersion(),
		"indexed_files":           files,
		"indexed_chunks_manifest": chunks,
		"zvec_doc_count":          docCount,
		"zvec_collection":         collectionName,
		"zvec_collection_path":    statusRelativePath(root, collectionPath),
		"zvec_open_ok":            zvecOpenOK,
		"index_meta_present":      zvec.IndexMetaPresent(p.Settings.IndexDir),
		"active_profile":          p.Settings.App.ActiveProfile,
		"indexing":                relativeIndexingMap(root, idx.ToIndexingMap()),
		"file_watcher":            p.fileWatcherStatus(),
		"search_performance":      p.searchStats.Snapshot(),
		"diagnostics":             diag,
	}
	if meta, err := zvec.ReadIndexMeta(p.Settings.IndexDir); err == nil && meta != nil {
		payload["index_embedding_profile"] = meta.EmbeddingProfile
		payload["index_embedding_dimensions"] = meta.EmbeddingDimensions
		payload["index_collection_name"] = meta.CollectionName
	}
	if identityMismatch {
		payload["identity_mismatch"] = true
		if identityErr != nil {
			payload["identity_mismatch_reason"] = identityErr.Error()
		}
	}
	if zvecErr != "" {
		payload["zvec_error"] = zvecErr
	}
	if p.embed == nil && profileErr == nil {
		payload["message"] = "embedding client failed to initialize"
	}
	if profileErr == nil && profile.Provider == "onnx" && profile.ModelPath != "" {
		payload["embedding_model_path"] = statusRelativePath(root, profile.ModelPath)
	}
	if profileErr != nil {
		payload["message"] = profileErr.Error()
	}
	p.startupMu.RLock()
	startupMsg := p.startupMsg
	p.startupMu.RUnlock()
	if startupMsg != "" && profileErr == nil {
		payload["message"] = startupMsg
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

func (p *Phase1) Reindex(ctx context.Context, req ReindexRequest) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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

func (p *Phase1) CheckUpdate(ctx context.Context) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.updateChecker == nil {
		p.updateChecker = update.NewChecker(p.Settings.GitHubRepo)
	}
	info := p.updateChecker.Check(ctx, version.Version)
	return marshal(info)
}

func (p *Phase1) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
	if err := p.embed.HealthCheck(ctx); err != nil {
		return fmt.Errorf("embeddings unreachable: %w", err)
	}
	if err := p.openZvecWithRecovery(); err != nil {
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
	p.startupMu.Lock()
	p.watcherInst = w
	p.startupMu.Unlock()
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
	if p.runIdentityMigrationIfNeeded() {
		return
	}
	p.StartAutoIndex()
}

func (p *Phase1) runIdentityMigrationIfNeeded() bool {
	if p.coordinator == nil {
		return false
	}
	profile, err := p.Settings.ActiveProfile()
	if err != nil {
		slog.Warn("identity migration check skipped", "err", err)
		return false
	}
	mismatch, meta, err := zvec.IndexIdentityMismatch(
		p.Settings.IndexDir,
		p.Settings.WorkspaceID,
		p.Settings.App.ActiveProfile,
		profile.Dimensions,
	)
	if err != nil && !mismatch {
		slog.Warn("identity migration check failed", "err", err)
		return false
	}
	if !mismatch {
		return false
	}

	if p.Settings.AutoIndexOnStart {
		slog.Info("workspace identity changed; force reindexing",
			"workspace_id", p.Settings.WorkspaceID,
			"workspace_root", p.Settings.WorkspaceRoot,
		)
		if _, err := p.coordinator.Start(true); err != nil {
			slog.Warn("identity migration reindex failed to start", "err", err)
		}
		return true
	}

	if err != nil {
		p.startupMu.Lock()
		p.startupMsg = fmt.Sprintf("index identity mismatch: %v — run reindex with force=true", err)
		p.startupMu.Unlock()
	} else {
		p.startupMu.Lock()
		p.startupMsg = "index identity mismatch — run reindex with force=true"
		p.startupMu.Unlock()
	}
	p.startupMu.RLock()
	msg := p.startupMsg
	p.startupMu.RUnlock()
	slog.Info(msg, "old_meta", meta)
	return true
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

	profile, err := p.Settings.ActiveProfile()
	if err != nil {
		slog.Warn("zvec-go migration reset skipped", "err", err)
		return false
	}
	identity := zvec.IndexIdentity{
		WorkspaceID:   p.Settings.WorkspaceID,
		WorkspaceRoot: p.Settings.WorkspaceRoot,
		Profile:       p.Settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}

	if err := zvec.ResetIndexForZvecMigration(p.Settings.IndexDir, meta, p.zvec, version.ZvecGoVersion, identity); err != nil {
		slog.Warn("zvec-go migration reset failed", "err", err)
		return false
	}

	if p.Settings.AutoIndexOnStart {
		if _, err := p.coordinator.Start(true); err != nil {
			slog.Warn("zvec-go migration reindex failed to start", "err", err)
		}
		return true
	}

	p.startupMu.Lock()
	p.startupMsg = "zvec-go updated — run reindex to rebuild the index"
	msg := p.startupMsg
	p.startupMu.Unlock()
	slog.Info(msg)
	return true
}

// StartAutoIndex triggers background indexing when AUTO_INDEX_ON_START is enabled.
func (p *Phase1) StartAutoIndex() {
	if p.coordinator == nil || !p.Settings.AutoIndexOnStart {
		return
	}
	if _, err := p.coordinator.Start(false); err != nil {
		slog.Warn("auto index on start failed", "err", err)
	}
}

// Close releases workspace resources (zvec collection handle).
func (p *Phase1) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return p.Shutdown(ctx)
}

// Shutdown waits for background indexing and in-flight searches to finish, then closes zvec.
func (p *Phase1) Shutdown(ctx context.Context) error {
	var err error
	p.shutdownOnce.Do(func() {
		indexIdle := true
		if p.coordinator != nil {
			if waitErr := p.coordinator.WaitForIdle(ctx); waitErr != nil {
				if err == nil {
					err = waitErr
				}
				indexIdle = false
			}
		}
		if waitErr := p.waitSearches(ctx); waitErr != nil && err == nil {
			err = waitErr
		}

		closeZvec := indexIdle
		if !closeZvec && p.coordinator != nil && p.coordinator.TryLockZvecForClose() {
			closeZvec = true
			defer p.coordinator.UnlockZvecForClose()
		} else if !closeZvec && p.coordinator == nil && !p.isIndexingRunning() {
			closeZvec = true
		}
		if closeZvec && p.zvec != nil {
			if closeErr := p.zvec.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
		}
		zvec.ReclaimCollectionLock(p.zvecCfg)
	})
	return err
}

func (p *Phase1) waitSearches(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		p.searchWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (p *Phase1) openZvecWithRecovery() error {
	if p.zvec.IsOpen() {
		return nil
	}
	err := p.zvec.Open()
	if err == nil || !lifecycle.IsZvecLockError(err) {
		return err
	}
	p.logZvecLockWarn(err)
	if recErr := p.recoverZvecLock(); recErr != nil && !errors.Is(recErr, errZvecRecoverySkipped) {
		return fmt.Errorf("zvec lock recovery failed: %w (open: %w)", recErr, err)
	}
	return p.zvec.Open()
}

func (p *Phase1) logZvecLockWarn(err error) {
	p.zvecLockWarnMu.Lock()
	defer p.zvecLockWarnMu.Unlock()
	if time.Since(p.lastZvecLockWarn) < 30*time.Second {
		return
	}
	p.lastZvecLockWarn = time.Now()
	slog.Warn("zvec open lock error, attempting recovery", "err", err)
}

func (p *Phase1) recoverZvecLock() error {
	if recErr := lifecycle.RecoverDuplicateStdio(p.Settings); recErr != nil {
		return recErr
	}
	zvec.ReclaimCollectionLock(p.zvecCfg)
	if p.coordinator != nil {
		if !p.coordinator.TryLockZvecForClose() {
			return errZvecRecoverySkipped
		}
		defer p.coordinator.UnlockZvecForClose()
	} else if p.isIndexingRunning() {
		return errZvecRecoverySkipped
	}
	if closeErr := p.zvec.Close(); closeErr != nil {
		return fmt.Errorf("zvec close during lock recovery: %w", closeErr)
	}
	return nil
}

func (p *Phase1) zvecSearchWithContext(ctx context.Context, vector []float32, limit int, pathGlob string) ([]zvec.SearchHit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		hits []zvec.SearchHit
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		hits, err := p.zvec.Search(vector, limit, pathGlob)
		ch <- result{hits: hits, err: err}
	}()
	select {
	case <-ctx.Done():
		// Wait for the zvec goroutine so searchWG is not released before Search
		// finishes (Shutdown must not close the collection mid-query).
		res := <-ch
		if res.err != nil {
			slog.Debug("zvec search finished after client cancel", "err", res.err)
		}
		return nil, ctx.Err()
	case res := <-ch:
		return res.hits, res.err
	}
}

func normalizeSearchLimit(limit int, topK *int) (int, error) {
	if limit == 0 && topK != nil {
		limit = *topK
	}
	if limit < 0 {
		return 0, ErrInvalidSearchLimit
	}
	if limit == 0 {
		limit = config.DefaultSearchLimit
	}
	if limit > config.DefaultMaxSearchLimit {
		limit = config.DefaultMaxSearchLimit
	}
	return limit, nil
}
