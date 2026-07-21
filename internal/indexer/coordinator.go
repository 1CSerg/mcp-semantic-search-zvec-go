package indexer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/scan"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/zvecerr"
)

// ErrAlreadyRunning is returned when Start is called while a job is active.
var ErrAlreadyRunning = errors.New("indexing already running")

// ErrCoordinatorClosed is returned when Start is called after Close.
var ErrCoordinatorClosed = errors.New("indexing coordinator closed")

// Embedder batches text into vectors during indexing.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
}

// Coordinator runs background indexing jobs.
type Coordinator struct {
	Settings *config.Settings
	Profile  config.EmbeddingProfile
	Embed    Embedder
	Zvec     zvec.Store
	ZvecCfg  zvec.Config

	progress *ProgressStore
	lock     *lock.Lock

	mu           sync.Mutex
	running      bool
	closed       bool
	curProgress  Progress
	lifecycleCtx context.Context
	zvecCloseMu  sync.Mutex
}

// NewCoordinator creates an indexing coordinator.
func NewCoordinator(settings *config.Settings, profile config.EmbeddingProfile, embed Embedder, store zvec.Store, cfg zvec.Config) *Coordinator {
	return &Coordinator{
		Settings: settings,
		Profile:  profile,
		Embed:    embed,
		Zvec:     store,
		ZvecCfg:  cfg,
		progress: NewProgressStore(settings.IndexDir),
		lock:     lock.New(settings.IndexDir, settings.App.Indexing.LockStaleSeconds),
	}
}

// SetLifecycleContext binds shutdown context for background indexing runs.
func (c *Coordinator) SetLifecycleContext(ctx context.Context) {
	c.mu.Lock()
	c.lifecycleCtx = ctx
	c.mu.Unlock()
}

func (c *Coordinator) runContext() context.Context {
	c.mu.Lock()
	ctx := c.lifecycleCtx
	c.mu.Unlock()
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

// ReleaseLock releases the cross-process index lock if this coordinator holds it.
func (c *Coordinator) ReleaseLock() error {
	return c.lock.Release()
}

// TryLockZvecForClose acquires the zvec close lock unless indexing holds it.
func (c *Coordinator) TryLockZvecForClose() bool {
	return c.zvecCloseMu.TryLock()
}

// UnlockZvecForClose releases the zvec close lock acquired by TryLockZvecForClose.
func (c *Coordinator) UnlockZvecForClose() {
	c.zvecCloseMu.Unlock()
}

// Close releases native chunker resources (tree-sitter C handles). It must be
// called only after WaitForIdle has returned and no indexing goroutine is
// running; it is intended for process shutdown. Safe to call multiple times.
//
// Lock order: zvecCloseMu → mu (same as indexing goroutine teardown).
func (c *Coordinator) Close() {
	c.zvecCloseMu.Lock()
	c.mu.Lock()
	if c.running {
		c.closed = true
		c.mu.Unlock()
		c.zvecCloseMu.Unlock()
		slog.Warn("coordinator Close called while indexing is running; skipping native resource teardown")
		return
	}
	c.closed = true
	c.mu.Unlock()
	chunk.CloseResources()
	c.zvecCloseMu.Unlock()
}

// IsRunning reports whether a job is active in this process.
func (c *Coordinator) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// WaitForIdle blocks until the background job finishes or ctx is canceled.
func (c *Coordinator) WaitForIdle(ctx context.Context) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !c.IsRunning() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// CurrentProgress returns in-memory or persisted progress.
func (c *Coordinator) CurrentProgress() Progress {
	c.mu.Lock()
	if c.running {
		p := c.curProgress
		c.mu.Unlock()
		return p
	}
	c.mu.Unlock()
	p, err := c.progress.Load()
	if err != nil {
		return Progress{State: StateIdle, Running: false, Message: err.Error()}
	}
	return p
}

// Start launches indexing in the background. Returns initial progress snapshot.
func (c *Coordinator) Start(force bool) (Progress, error) {
	if err := RecoverStalledProgress(c.Settings.IndexDir, c.Settings.App.Indexing.StallSeconds, nil); err != nil {
		slog.Warn("recover stalled indexing progress failed", "err", err)
	}
	_ = RecoverInterruptedProgress(c.Settings.IndexDir)

	if !force {
		if need, err := c.manifestZvecDesync(); err != nil {
			return Progress{State: StateIdle, Running: false}, err
		} else if need {
			force = true
			slog.Warn("manifest populated but zvec empty; forcing full reindex")
		}
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Progress{State: StateIdle, Running: false}, ErrCoordinatorClosed
	}
	if c.running {
		p := c.curProgress
		c.mu.Unlock()
		return p, ErrAlreadyRunning
	}
	c.mu.Unlock()

	if err := c.lock.TryAcquire(); err != nil {
		return Progress{State: StateIdle, Running: false}, err
	}

	c.mu.Lock()
	if c.closed {
		_ = c.lock.Release()
		c.mu.Unlock()
		return Progress{State: StateIdle, Running: false}, ErrCoordinatorClosed
	}
	if c.running {
		_ = c.lock.Release()
		p := c.curProgress
		c.mu.Unlock()
		return p, ErrAlreadyRunning
	}
	c.running = true
	c.curProgress = StartRunning(force)
	c.mu.Unlock()

	p := c.curProgress
	if err := c.progress.Save(p); err != nil {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		_ = c.lock.Release()
		return p, err
	}

	go func() {
		c.zvecCloseMu.Lock()
		var filesFailed, finishFiles, finishChunks int
		var runErr error
		defer func() {
			if r := recover(); r != nil {
				runErr = fmt.Errorf("indexing panic: %v", r)
				slog.Error("indexing goroutine panic", "panic", r)
			}
			_ = c.lock.Release()
			c.zvecCloseMu.Unlock()
			c.mu.Lock()
			c.running = false
			if runErr != nil {
				if IsContextInterrupt(runErr) {
					c.curProgress = FinishInterrupted(c.curProgress)
				} else {
					c.curProgress = FinishError(c.curProgress, runErr)
				}
			} else if filesFailed > 0 {
				c.curProgress = FinishIdleWithWarnings(c.curProgress, filesFailed)
				if finishFiles > 0 {
					c.curProgress.FilesTotal = finishFiles
					c.curProgress.FilesDone = finishFiles
				}
				c.curProgress.ChunksIndexed = finishChunks
			} else {
				c.curProgress = FinishIdle(c.curProgress, finishFiles, finishChunks)
			}
			finalProgress := c.curProgress
			c.mu.Unlock()
			if err := c.progress.Save(finalProgress); err != nil {
				slog.Warn("persist final indexing progress failed", "err", err)
			}
		}()

		filesFailed, finishFiles, finishChunks, runErr = c.run(c.runContext(), force)
	}()

	return p, nil
}

func (c *Coordinator) run(ctx context.Context, force bool) (filesFailed int, finishFiles, finishChunks int, err error) {
	if err := os.MkdirAll(c.Settings.IndexDir, 0o755); err != nil {
		return 0, 0, 0, err
	}

	if err := zvec.ReconcileIndex(
		c.Settings.IndexDir,
		zvec.IndexIdentity{
			WorkspaceID:      c.Settings.WorkspaceID,
			WorkspaceRoot:    c.Settings.WorkspaceRoot,
			Profile:          c.Settings.App.ActiveProfile,
			Dimensions:       c.Profile.Dimensions,
			ChunkingVersion:  c.Settings.App.Indexing.Chunking.Version,
			ChunkingStrategy: c.Settings.App.Indexing.Chunking.Strategy,
		},
		force,
		c.Zvec,
	); err != nil {
		return 0, 0, 0, err
	}

	manifestPath := filepath.Join(c.Settings.IndexDir, "manifest.db")
	manStore, err := manifest.Open(manifestPath)
	if err != nil {
		return 0, 0, 0, err
	}
	defer func() { _ = manStore.Close() }()

	if force {
		if err := c.Zvec.WipeCollection(); err != nil && !isZvecUnavailable(err) {
			return 0, 0, 0, err
		}
		if err := manStore.Clear(); err != nil {
			return 0, 0, 0, err
		}
	}

	scanResult, err := scan.Discover(scan.Options{
		Root:       c.Settings.WorkspaceRoot,
		Extensions: c.Settings.App.Indexing.Extensions,
		SkipDirs:   c.Settings.App.Indexing.SkipDirs,
	})
	if err != nil {
		return 0, 0, 0, err
	}
	files := scanResult.Files

	discovered := make(map[string]struct{}, len(files))
	stallWatch := NewStallWatcher(c.Settings.App.Indexing.StallSeconds)

	c.updateProgress(func(p *Progress) {
		p.FilesTotal = len(files)
		p.ScanMethod = scanResult.Method
		p.ScanWarnings = append([]string(nil), scanResult.Warnings...)
		p.SkippedPaths = append([]string(nil), scanResult.SkippedPaths...)
		msg := fmt.Sprintf("discovered %d files via %s", len(files), scanResult.Method)
		if len(scanResult.Warnings) > 0 {
			msg += "; " + scanResult.Warnings[0]
		}
		if len(scanResult.SkippedPaths) > 0 {
			msg += fmt.Sprintf("; %d paths skipped", len(scanResult.SkippedPaths))
		}
		p.Message = msg
	})
	stallWatch.Touch()

	for i, rel := range files {
		select {
		case <-ctx.Done():
			return 0, 0, 0, ctx.Err()
		default:
		}
		if err := stallWatch.Check(); err != nil {
			return 0, 0, 0, err
		}
		discovered[rel] = struct{}{}
		c.updateProgress(func(p *Progress) {
			p.FilesDone = i
			p.CurrentFile = rel
			p.Message = fmt.Sprintf("indexing %s", rel)
		})
		stallWatch.Touch()

		if err := c.indexFile(ctx, manStore, rel, force); err != nil {
			if isFatalIndexingError(err) {
				return 0, 0, 0, fmt.Errorf("%s: %w", rel, err)
			}
			if isPerFileSkippable(err) {
				if errors.Is(err, os.ErrNotExist) {
					if purgeErr := c.purgeRemovedFile(manStore, rel); purgeErr != nil {
						filesFailed++
						slog.Warn("purge vanished file failed", "path", rel, "err", purgeErr)
						c.updateProgress(func(p *Progress) {
							p.FilesFailed = filesFailed
							AppendFailedFile(p, rel)
						})
					} else {
						delete(discovered, rel)
						slog.Info("index file vanished during run; purged stale index", "path", rel)
					}
				} else {
					filesFailed++
					slog.Warn("index file skipped", "path", rel, "err", err)
					c.updateProgress(func(p *Progress) {
						p.FilesFailed = filesFailed
						AppendFailedFile(p, rel)
					})
				}
				stallWatch.Touch()
				continue
			}
			return 0, 0, 0, fmt.Errorf("%s: %w", rel, err)
		}
		c.refreshChunkProgress(manStore)
		stallWatch.Touch()
	}

	if !force {
		existing, err := manStore.List()
		if err != nil {
			return 0, 0, 0, err
		}
		for _, e := range existing {
			if _, ok := discovered[e.RelativePath]; ok {
				continue
			}
			if len(e.DocIDs) > 0 {
				if err := c.Zvec.DeleteByIDs(e.DocIDs); err != nil && !isZvecUnavailable(err) {
					return 0, 0, 0, err
				}
			}
			if err := manStore.Delete(e.RelativePath); err != nil {
				return 0, 0, 0, err
			}
		}
	}

	c.updateProgress(func(p *Progress) {
		p.FilesDone = len(files)
		p.CurrentFile = ""
	})
	c.refreshChunkProgress(manStore)
	stallWatch.Touch()

	finishFiles, finishChunks, statsErr := manifestStats(manStore)
	if statsErr != nil {
		// Fall back to the work done in this run instead of reporting 0/0,
		// which would wipe files_total/files_done on the success path.
		c.mu.Lock()
		finishChunks = c.curProgress.ChunksIndexed
		c.mu.Unlock()
		finishFiles = len(files)
		slog.Warn("manifest stats at finish failed", "err", statsErr)
	}
	return filesFailed, finishFiles, finishChunks, nil
}

func (c *Coordinator) indexFile(ctx context.Context, manStore *manifest.Store, rel string, force bool) error {
	abs, err := chunk.ResolveWithinRoot(c.Settings.WorkspaceRoot, rel)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}

	var old *manifest.FileEntry
	if !force {
		old, err = manStore.Get(rel)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("manifest get %s: %w", rel, err)
		}
		if old != nil && old.MtimeNs == info.ModTime().UnixNano() && old.Size == info.Size() {
			return nil
		}
	}

	chunkOpts := chunk.OptionsFromConfig(c.Settings.App.Indexing, c.Profile)
	batchSize := c.Profile.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}
	counter, err := token.NewCounter(c.Profile, c.Settings.WorkspaceRoot)
	if err != nil {
		return err
	}
	contextPrefix := c.Settings.App.Indexing.Chunking.ContextPrefix

	var docIDs []string
	chunkCount, err := chunk.ProcessBatches(c.Settings.WorkspaceRoot, rel, chunkOpts, counter, batchSize, func(batch []zvec.Chunk) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		texts := make([]string, len(batch))
		for i, ch := range batch {
			texts[i] = chunk.EmbedTextForChunk(ch.RelativePath, ch.ParentScope, ch.Snippet, contextPrefix)
		}
		vectors, err := c.embedTexts(ctx, texts)
		if err != nil {
			return fatalEmbedErr(err)
		}
		if err := c.Zvec.UpsertChunks(batch, vectors); err != nil && !isZvecUnavailable(err) {
			return err
		}
		for _, ch := range batch {
			docIDs = append(docIDs, ch.DocID)
		}
		return nil
	})
	if err != nil {
		if len(docIDs) > 0 {
			if delErr := c.Zvec.DeleteByIDs(docIDs); delErr != nil && !isZvecUnavailable(delErr) {
				slog.Warn("index file rollback incomplete", "path", rel, "index_err", err, "rollback_err", delErr)
			}
		}
		return err
	}

	if chunkCount == 0 {
		if old != nil && len(old.DocIDs) > 0 {
			if err := c.Zvec.DeleteByIDs(old.DocIDs); err != nil && !isZvecUnavailable(err) {
				return err
			}
		}
		if err := manStore.Delete(rel); err != nil {
			return fmt.Errorf("manifest delete %s: %w", rel, err)
		}
		return nil
	}

	if err := manStore.Upsert(manifest.FileEntry{
		RelativePath: rel,
		MtimeNs:      info.ModTime().UnixNano(),
		Size:         info.Size(),
		ChunkCount:   chunkCount,
		DocIDs:       docIDs,
	}); err != nil {
		if delErr := c.Zvec.DeleteByIDs(docIDs); delErr != nil && !isZvecUnavailable(delErr) {
			slog.Warn("manifest upsert failed; zvec rollback incomplete", "path", rel, "manifest_err", err, "rollback_err", delErr)
		}
		return fmt.Errorf("manifest upsert %s: %w", rel, err)
	}

	if old != nil && len(old.DocIDs) > 0 {
		stale := staleDocIDs(old.DocIDs, docIDs)
		if len(stale) > 0 {
			if err := c.deleteStaleVectors(stale); err != nil {
				if delErr := c.Zvec.DeleteByIDs(docIDs); delErr != nil && !isZvecUnavailable(delErr) {
					slog.Warn("stale vector rollback incomplete", "path", rel, "rollback_err", delErr)
				}
				if old != nil {
					if upErr := manStore.Upsert(*old); upErr != nil {
						slog.Warn("manifest rollback after stale delete failed", "path", rel, "err", upErr)
					}
				} else if delErr := manStore.Delete(rel); delErr != nil {
					slog.Warn("manifest rollback delete failed", "path", rel, "err", delErr)
				}
				return err
			}
		}
	}
	return nil
}

func (c *Coordinator) purgeRemovedFile(manStore *manifest.Store, rel string) error {
	entry, err := manStore.Get(rel)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("manifest get %s: %w", rel, err)
	}
	if entry == nil {
		return nil
	}
	if len(entry.DocIDs) > 0 {
		if err := c.Zvec.DeleteByIDs(entry.DocIDs); err != nil && !isZvecUnavailable(err) {
			return err
		}
	}
	if err := manStore.Delete(rel); err != nil {
		return fmt.Errorf("manifest delete %s: %w", rel, err)
	}
	return nil
}

func (c *Coordinator) embedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	batchSize := c.Profile.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}
	if len(texts) <= batchSize {
		return c.Embed.Embed(ctx, texts)
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := c.Embed.Embed(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (c *Coordinator) deleteStaleVectors(ids []string) error {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := c.Zvec.DeleteByIDs(ids)
		if err == nil || isZvecUnavailable(err) {
			return nil
		}
		lastErr = err
		slog.Warn("stale vector delete failed", "attempt", attempt, "count", len(ids), "err", err)
		time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
	}
	return lastErr
}

// staleDocIDs returns old document IDs that are not present in the new set.
func staleDocIDs(oldIDs, newIDs []string) []string {
	if len(oldIDs) == 0 {
		return nil
	}
	keep := make(map[string]struct{}, len(newIDs))
	for _, id := range newIDs {
		keep[id] = struct{}{}
	}
	var stale []string
	for _, id := range oldIDs {
		if _, ok := keep[id]; !ok {
			stale = append(stale, id)
		}
	}
	return stale
}

func (c *Coordinator) updateProgress(fn func(*Progress)) {
	c.mu.Lock()
	fn(&c.curProgress)
	c.curProgress.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	p := c.curProgress
	c.mu.Unlock()
	if err := c.progress.Save(p); err != nil {
		slog.Warn("persist indexing progress failed", "err", err)
	}
}

func (c *Coordinator) refreshChunkProgress(manStore *manifest.Store) {
	_, chunks, err := manifestStats(manStore)
	if err != nil {
		slog.Debug("manifest stats refresh failed", "err", err)
		return
	}
	c.updateProgress(func(p *Progress) {
		p.ChunksIndexed = chunks
	})
}

func manifestStats(manStore *manifest.Store) (files, chunks int, err error) {
	if manStore == nil {
		return 0, 0, fmt.Errorf("manifest store is nil")
	}
	return manStore.Stats()
}

func isZvecUnavailable(err error) bool {
	return errors.Is(err, zvec.ErrNotLinked)
}

func (c *Coordinator) manifestZvecDesync() (bool, error) {
	manifestPath := filepath.Join(c.Settings.IndexDir, "manifest.db")
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	manStore, err := manifest.Open(manifestPath)
	if err != nil {
		return false, err
	}
	defer func() { _ = manStore.Close() }()
	_, chunks, err := manStore.Stats()
	if err != nil {
		return false, err
	}
	if chunks == 0 {
		return false, nil
	}
	if !c.Zvec.IsOpen() {
		if err := c.Zvec.Open(); err != nil {
			if errors.Is(err, zvec.ErrCollectionMissing) || zvecerr.IsLockError(err) {
				return true, nil
			}
			return false, err
		}
	}
	docCount, err := c.Zvec.DocCount()
	if err != nil {
		if zvecerr.IsLockError(err) {
			return true, nil
		}
		return false, err
	}
	return docCount == 0, nil
}
