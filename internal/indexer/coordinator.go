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
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/scan"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

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
	curProgress  Progress
	lifecycleCtx context.Context
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
	c.lifecycleCtx = ctx
}

func (c *Coordinator) runContext() context.Context {
	if c.lifecycleCtx != nil {
		return c.lifecycleCtx
	}
	return context.Background()
}

// IsRunning reports whether a job is active in this process.
func (c *Coordinator) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
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
	_ = RecoverStalledProgress(c.Settings.IndexDir, c.Settings.App.Indexing.StallSeconds, nil)

	c.mu.Lock()
	if c.running {
		p := c.curProgress
		c.mu.Unlock()
		return p, fmt.Errorf("indexing already running")
	}
	c.mu.Unlock()

	if err := c.lock.TryAcquire(); err != nil {
		return Progress{State: StateIdle, Running: false}, err
	}

	c.mu.Lock()
	if c.running {
		_ = c.lock.Release()
		p := c.curProgress
		c.mu.Unlock()
		return p, fmt.Errorf("indexing already running")
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
		filesFailed, finishFiles, finishChunks, err := c.run(c.runContext(), force)
		_ = c.lock.Release()
		c.mu.Lock()
		c.running = false
		if err != nil {
			c.curProgress = FinishError(c.curProgress, err)
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
		if err := c.progress.Save(c.curProgress); err != nil {
			slog.Warn("persist final indexing progress failed", "err", err)
		}
		c.mu.Unlock()
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
			WorkspaceID:   c.Settings.WorkspaceID,
			WorkspaceRoot: c.Settings.WorkspaceRoot,
			Profile:       c.Settings.App.ActiveProfile,
			Dimensions:    c.Profile.Dimensions,
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
	defer manStore.Close()

	if force {
		if err := manStore.Clear(); err != nil {
			return 0, 0, 0, err
		}
		if err := c.Zvec.WipeCollection(); err != nil && !isZvecUnavailable(err) {
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
				filesFailed++
				slog.Warn("index file skipped", "path", rel, "err", err)
				c.updateProgress(func(p *Progress) {
					p.FilesFailed = filesFailed
					AppendFailedFile(p, rel)
				})
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
	abs := filepath.Join(c.Settings.WorkspaceRoot, filepath.FromSlash(rel))
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

	chunks, err := chunk.ReadAndChunk(c.Settings.WorkspaceRoot, rel, chunk.Options{
		MaxFileBytes:         c.Settings.App.Indexing.MaxFileBytes,
		StreamThresholdBytes: c.Settings.App.Indexing.StreamChunkThresholdBytes,
		MaxLineBytes:         c.Settings.App.Indexing.MaxLineBytes,
	})
	if err != nil {
		return err
	}
	if old != nil && len(old.DocIDs) > 0 {
		if err := c.Zvec.DeleteByIDs(old.DocIDs); err != nil && !isZvecUnavailable(err) {
			return err
		}
	}

	if len(chunks) == 0 {
		if err := manStore.Delete(rel); err != nil {
			return fmt.Errorf("manifest delete %s: %w", rel, err)
		}
		return nil
	}

	texts := make([]string, len(chunks))
	for i, ch := range chunks {
		texts[i] = ch.Snippet
	}
	vectors, err := c.Embed.Embed(ctx, texts)
	if err != nil {
		return fatalEmbedErr(err)
	}
	if err := c.Zvec.UpsertChunks(chunks, vectors); err != nil && !isZvecUnavailable(err) {
		return err
	}

	docIDs := make([]string, len(chunks))
	for i, ch := range chunks {
		docIDs[i] = ch.DocID
	}
	return manStore.Upsert(manifest.FileEntry{
		RelativePath: rel,
		MtimeNs:      info.ModTime().UnixNano(),
		Size:         info.Size(),
		ChunkCount:   len(chunks),
		DocIDs:       docIDs,
	})
}

func (c *Coordinator) updateProgress(fn func(*Progress)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(&c.curProgress)
	c.curProgress.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := c.progress.Save(c.curProgress); err != nil {
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
	return err == zvec.ErrNotLinked
}
