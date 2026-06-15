package indexer

import (
	"context"
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

	mu          sync.Mutex
	running     bool
	curProgress Progress
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
	_ = RecoverStalledProgress(c.Settings.IndexDir, c.Settings.App.Indexing.StallSeconds)

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
		filesFailed, err := c.run(context.Background(), force)
		_ = c.lock.Release()
		c.mu.Lock()
		c.running = false
		if err != nil {
			c.curProgress = FinishError(c.curProgress, err)
		} else if filesFailed > 0 {
			c.curProgress = FinishIdleWithWarnings(c.curProgress, filesFailed)
		} else {
			files, chunks := c.countStats()
			c.curProgress = FinishIdle(c.curProgress, files, chunks)
		}
		_ = c.progress.Save(c.curProgress)
		c.mu.Unlock()
	}()

	return p, nil
}

func (c *Coordinator) run(ctx context.Context, force bool) (filesFailed int, err error) {
	if err := os.MkdirAll(c.Settings.IndexDir, 0o755); err != nil {
		return 0, err
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
		return 0, err
	}

	manifestPath := filepath.Join(c.Settings.IndexDir, "manifest.db")
	manStore, err := manifest.Open(manifestPath)
	if err != nil {
		return 0, err
	}
	defer manStore.Close()

	if force {
		if err := manStore.Clear(); err != nil {
			return 0, err
		}
		if err := c.Zvec.WipeCollection(); err != nil && !isZvecUnavailable(err) {
			return 0, err
		}
	}

	files, err := scan.Discover(scan.Options{
		Root:       c.Settings.WorkspaceRoot,
		Extensions: c.Settings.App.Indexing.Extensions,
		SkipDirs:   c.Settings.App.Indexing.SkipDirs,
	})
	if err != nil {
		return 0, err
	}

	discovered := make(map[string]struct{}, len(files))
	totalChunks := 0
	heartbeat := time.Duration(c.Settings.App.Indexing.HeartbeatSeconds * float64(time.Second))
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	lastBeat := time.Now()
	stallWatch := NewStallWatcher(c.Settings.App.Indexing.StallSeconds)

	c.updateProgress(func(p *Progress) {
		p.FilesTotal = len(files)
		p.Message = fmt.Sprintf("discovered %d files", len(files))
	})
	stallWatch.Touch()

	for i, rel := range files {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		if err := stallWatch.Check(); err != nil {
			return 0, err
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
				return 0, fmt.Errorf("%s: %w", rel, err)
			}
			if isPerFileSkippable(err) {
				filesFailed++
				slog.Warn("index file skipped", "path", rel, "err", err)
				c.updateProgress(func(p *Progress) {
					p.FilesFailed = filesFailed
				})
				stallWatch.Touch()
				continue
			}
			return 0, fmt.Errorf("%s: %w", rel, err)
		}
		entry, err := manStore.Get(rel)
		if err == nil {
			totalChunks += entry.ChunkCount
			c.updateProgress(func(p *Progress) {
				p.ChunksIndexed = totalChunks
			})
			stallWatch.Touch()
		}

		if time.Since(lastBeat) >= heartbeat {
			_ = c.lock.Heartbeat()
			lastBeat = time.Now()
		}
	}

	if !force {
		existing, err := manStore.List()
		if err != nil {
			return 0, err
		}
		for _, e := range existing {
			if _, ok := discovered[e.RelativePath]; ok {
				continue
			}
			if len(e.DocIDs) > 0 {
				if err := c.Zvec.DeleteByIDs(e.DocIDs); err != nil && !isZvecUnavailable(err) {
					return 0, err
				}
			}
			if err := manStore.Delete(e.RelativePath); err != nil {
				return 0, err
			}
		}
	}

	c.updateProgress(func(p *Progress) {
		p.FilesDone = len(files)
		p.CurrentFile = ""
	})
	stallWatch.Touch()
	return filesFailed, nil
}

func (c *Coordinator) indexFile(ctx context.Context, manStore *manifest.Store, rel string, force bool) error {
	abs := filepath.Join(c.Settings.WorkspaceRoot, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}

	var old *manifest.FileEntry
	if !force {
		old, _ = manStore.Get(rel)
		if old != nil && old.MtimeNs == info.ModTime().UnixNano() && old.Size == info.Size() {
			return nil
		}
	}

	chunks, err := chunk.ReadAndChunk(c.Settings.WorkspaceRoot, rel, chunk.Options{})
	if err != nil {
		return err
	}
	if old != nil && len(old.DocIDs) > 0 {
		if err := c.Zvec.DeleteByIDs(old.DocIDs); err != nil && !isZvecUnavailable(err) {
			return err
		}
	}

	if len(chunks) == 0 {
		_ = manStore.Delete(rel)
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
	_ = c.progress.Save(c.curProgress)
}

func (c *Coordinator) countStats() (files, chunks int) {
	manStore, err := manifest.Open(filepath.Join(c.Settings.IndexDir, "manifest.db"))
	if err != nil {
		return 0, 0
	}
	defer manStore.Close()
	f, ch, err := manStore.Stats()
	if err != nil {
		return 0, 0
	}
	return f, ch
}

func isZvecUnavailable(err error) bool {
	return err == zvec.ErrNotLinked
}
