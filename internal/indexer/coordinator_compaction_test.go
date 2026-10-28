package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

// compactRecorder captures the maintenance events of one indexing run in order, so
// a test can assert both "was the collection maintained" and "did that happen
// before close/promote and before Running=false".
type compactRecorder struct {
	mu     sync.Mutex
	events []string
	err    error
	// coordinator lets Compact observe whether the run was still in progress.
	coordinator *Coordinator
}

func (r *compactRecorder) add(event string) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *compactRecorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func (r *compactRecorder) reset() {
	r.mu.Lock()
	r.events = nil
	r.mu.Unlock()
}

func (r *compactRecorder) count(event string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e == event {
			n++
		}
	}
	return n
}

func (r *compactRecorder) setErr(err error) {
	r.mu.Lock()
	r.err = err
	r.mu.Unlock()
}

// compactableZvec is a memZvec that also implements zvec.Compactor, recording every
// maintenance and close call under its label.
//
// It tracks a dirty flag the way CollectionStore does, so a test can distinguish "the
// coordinator asked" from "the store actually had work": the event carries both.
type compactableZvec struct {
	*memZvec
	rec   *compactRecorder
	label string
	dirty bool
}

func (s *compactableZvec) markDirty() {
	s.mu.Lock()
	s.dirty = true
	s.mu.Unlock()
}

func (s *compactableZvec) Compact() error {
	running := false
	if s.rec.coordinator != nil {
		running = s.rec.coordinator.IsRunning()
	}
	s.mu.Lock()
	dirty := s.dirty
	s.dirty = false
	s.mu.Unlock()
	s.rec.add(fmt.Sprintf("%s:compact running=%t dirty=%t", s.label, running, dirty))
	s.rec.mu.Lock()
	err := s.rec.err
	s.rec.mu.Unlock()
	return err
}

func (s *compactableZvec) UpsertChunks(chunks []zvec.Chunk, vectors [][]float32) error {
	if err := s.memZvec.UpsertChunks(chunks, vectors); err != nil {
		return err
	}
	s.markDirty()
	return nil
}

func (s *compactableZvec) DeleteByIDs(ids []string) error {
	if err := s.memZvec.DeleteByIDs(ids); err != nil {
		return err
	}
	s.markDirty()
	return nil
}

func (s *compactableZvec) WipeCollection() error {
	if err := s.memZvec.WipeCollection(); err != nil {
		return err
	}
	s.mu.Lock()
	s.dirty = false
	s.mu.Unlock()
	return nil
}

func (s *compactableZvec) Close() error {
	s.rec.add(s.label + ":close")
	return nil
}

// newCompactableZvec returns a compacting mem store registered with rec.
func newCompactableZvec(rec *compactRecorder, label string) *compactableZvec {
	return &compactableZvec{memZvec: newMemZvec(), rec: rec, label: label}
}

// compactionTestWorkspace builds a coordinator over a single-file workspace.
func compactionTestWorkspace(t *testing.T, rec *compactRecorder, active zvec.Store) (*Coordinator, string) {
	t.Helper()
	root := tempDir(t)
	indexDir := filepath.Join(root, "index")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "auth.go"), []byte("package pkg\n\nfunc Auth() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := &config.Settings{
		WorkspaceRoot: root,
		WorkspaceID:   "test-ws",
		IndexDir:      indexDir,
		App: config.AppConfig{
			ActiveProfile: "test",
			Indexing: config.IndexingConfig{
				Extensions:       []string{".go"},
				SkipDirs:         []string{".git", "node_modules"},
				LockStaleSeconds: 300,
			},
		},
	}
	profile := config.EmbeddingProfile{Provider: "openai_compatible", Dimensions: 4, BatchSize: 1}
	cfg := zvec.Config{IndexDir: indexDir, WorkspaceRoot: root, ProfileName: "test", Dimensions: 4}
	c := NewCoordinator(settings, profile, &mockEmbedder{dims: 4}, active, cfg)
	if rec != nil {
		rec.coordinator = c
	}
	registerCoordinatorTestCleanup(t, c)
	return c, root
}

// A completed run that changed the collection must be finalized, and the
// maintenance must finish before Running=false is published: callers treat
// Running=false as "the index dir is quiescent".
func TestCoordinatorCompactsChangedRunBeforeIdle(t *testing.T) {
	rec := &compactRecorder{}
	active := newCompactableZvec(rec, "active")
	c, _ := compactionTestWorkspace(t, rec, active)

	if _, err := c.Start(false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	if p := c.CurrentProgress(); p.State != StateIdle || p.Running {
		t.Fatalf("final progress=%+v", p)
	}
	events := rec.list()
	if len(events) != 1 || events[0] != "active:compact running=true dirty=true" {
		t.Fatalf("events=%v want exactly one compaction while the run was still active", events)
	}
}

// A store without the optional Compactor keeps working: maintenance is optional, and
// the required Store interface must not grow.
func TestCoordinatorRunsWithoutCompactor(t *testing.T) {
	c, _ := compactionTestWorkspace(t, nil, newMemZvec())

	if _, err := c.Start(false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)
	if p := c.CurrentProgress(); p.State != StateIdle {
		t.Fatalf("final progress=%+v", p)
	}
}

// A maintenance failure is advisory: it must not turn a successful run into an error
// state, and it must not lose the index.
func TestCoordinatorCompactionErrorDoesNotFailRun(t *testing.T) {
	rec := &compactRecorder{}
	rec.setErr(errors.New("native optimize failed"))
	active := newCompactableZvec(rec, "active")
	c, _ := compactionTestWorkspace(t, rec, active)

	if _, err := c.Start(false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	if p := c.CurrentProgress(); p.State != StateIdle || p.Error != "" {
		t.Fatalf("maintenance failure leaked into progress: %+v", p)
	}
	if rec.count("active:compact running=true dirty=true") != 1 {
		t.Fatalf("events=%v want one compaction attempt", rec.list())
	}
	if n, err := active.DocCount(); err != nil || n == 0 {
		t.Fatalf("index lost after maintenance failure: n=%d err=%v", n, err)
	}
}

// A run that only deleted vectors (removed files) still has something to reclaim.
func TestCoordinatorCompactsDeleteOnlyRun(t *testing.T) {
	rec := &compactRecorder{}
	active := newCompactableZvec(rec, "active")
	c, root := compactionTestWorkspace(t, rec, active)

	if _, err := c.Start(false); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	waitCoordinatorIdle(t, c)
	rec.reset()

	if err := os.Remove(filepath.Join(root, "pkg", "auth.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Start(false); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	if p := c.CurrentProgress(); p.State != StateIdle {
		t.Fatalf("final progress=%+v", p)
	}
	if n := rec.count("active:compact running=true dirty=true"); n != 1 {
		t.Fatalf("delete-only run compacted %d times, want 1; events=%v", n, rec.list())
	}
	if n, err := active.DocCount(); err != nil || n != 0 {
		t.Fatalf("doc count=%d err=%v want 0", n, err)
	}
}

// A run whose only change is the replay of a pending cleanup journal must also be
// finalized, otherwise those deletes never reclaim their blocks.
func TestCoordinatorCompactsJournalOnlyRun(t *testing.T) {
	rec := &compactRecorder{}
	active := newCompactableZvec(rec, "active")
	c, _ := compactionTestWorkspace(t, rec, active)

	journal, err := manifest.OpenCleanupJournal(c.Settings.IndexDir)
	if err != nil {
		t.Fatalf("open cleanup journal: %v", err)
	}
	if err := journal.Append([]string{"ghost-doc"}); err != nil {
		t.Fatalf("append journal: %v", err)
	}

	if _, err := c.Start(false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	if p := c.CurrentProgress(); p.State != StateIdle {
		t.Fatalf("final progress=%+v", p)
	}
	if n := rec.count("active:compact running=true dirty=true"); n != 1 {
		t.Fatalf("journal-only run compacted %d times, want 1; events=%v", n, rec.list())
	}
}

// Force reindex must maintain the staging build before it is closed and promoted,
// and must not maintain the active collection it is about to replace.
func TestCoordinatorForceReindexCompactsStagingBeforeCloseAndPromote(t *testing.T) {
	rec := &compactRecorder{}
	active := newCompactableZvec(rec, "active")
	c, _ := compactionTestWorkspace(t, rec, active)

	staging := newCompactableZvec(rec, "staging")
	c.forceStagingOpen = func(zvec.Config) (zvec.Store, error) {
		return staging, nil
	}
	c.forcePromoteStaging = func(s zvec.Store) error {
		rec.add("staging:promote")
		st, ok := s.(*compactableZvec)
		if !ok {
			return fmt.Errorf("unexpected staging store %T", s)
		}
		st.mu.Lock()
		copied := make(map[string]zvec.Chunk, len(st.chunks))
		for k, v := range st.chunks {
			copied[k] = v
		}
		st.mu.Unlock()
		active.mu.Lock()
		active.chunks = copied
		active.mu.Unlock()
		return zvec.PromoteStagingManifest(c.Settings.IndexDir)
	}

	if _, err := c.Start(true); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	if p := c.CurrentProgress(); p.State != StateIdle {
		t.Fatalf("final progress=%+v", p)
	}
	want := []string{"staging:compact running=true dirty=true", "staging:close", "staging:promote"}
	got := rec.list()
	if len(got) != len(want) {
		t.Fatalf("events=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d=%q want %q (full: %v)", i, got[i], want[i], got)
		}
	}
	if n, err := active.DocCount(); err != nil || n == 0 {
		t.Fatalf("promoted index empty: n=%d err=%v", n, err)
	}
}

// A failed staging build is never published, so it must not pay for a full rebuild
// of a result that is about to be discarded.
func TestCoordinatorFailedForceReindexSkipsCompaction(t *testing.T) {
	rec := &compactRecorder{}
	active := newMemZvec()
	c, _ := compactionTestWorkspace(t, rec, active)

	staging := newCompactableZvec(rec, "staging")
	c.forceStagingOpen = func(zvec.Config) (zvec.Store, error) {
		return staging, nil
	}
	c.forcePromoteStaging = func(zvec.Store) error {
		rec.add("staging:promote")
		return nil
	}
	c.Embed = &batchFailEmbedder{dims: 4, failAfter: 0}

	if _, err := c.Start(true); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	if p := c.CurrentProgress(); p.State != StateError {
		t.Fatalf("expected failed force reindex, got %+v", p)
	}
	if n := rec.count("staging:compact running=true"); n != 0 {
		t.Fatalf("failed staging build was compacted %d times; events=%v", n, rec.list())
	}
}

// Without staging the run writes straight into the active collection, so that is
// what must be finalized.
func TestCoordinatorForceReindexFallbackCompactsActive(t *testing.T) {
	rec := &compactRecorder{}
	active := newCompactableZvec(rec, "active")
	c, _ := compactionTestWorkspace(t, rec, active)
	c.forceStagingOpen = func(zvec.Config) (zvec.Store, error) {
		return nil, zvec.ErrNotLinked
	}

	if _, err := c.Start(true); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	if p := c.CurrentProgress(); p.State != StateIdle {
		t.Fatalf("final progress=%+v", p)
	}
	if n := rec.count("active:compact running=true dirty=true"); n != 1 {
		t.Fatalf("fallback run compacted active %d times, want 1; events=%v", n, rec.list())
	}
	if n, err := active.DocCount(); err != nil || n == 0 {
		t.Fatalf("fallback index empty: n=%d err=%v", n, err)
	}
}

// On the force path the active collection is discarded on success, so it must not pay
// for a rebuild. But the run replays the cleanup journal into the ACTIVE collection
// before staging is chosen, and if the force run then fails that active collection is
// still serving — its journal deletes must be reclaimed, or the dirty flag dies with
// the process and the blocks are never freed.
func TestCoordinatorFailedForceReindexStillCompactsActiveJournalChanges(t *testing.T) {
	rec := &compactRecorder{}
	active := newCompactableZvec(rec, "active")
	c, _ := compactionTestWorkspace(t, rec, active)

	// A pending journal entry makes the run delete from the active collection first.
	journal, err := manifest.OpenCleanupJournal(c.Settings.IndexDir)
	if err != nil {
		t.Fatalf("open cleanup journal: %v", err)
	}
	if err := journal.Append([]string{"ghost-doc"}); err != nil {
		t.Fatalf("append journal: %v", err)
	}

	staging := newCompactableZvec(rec, "staging")
	c.forceStagingOpen = func(zvec.Config) (zvec.Store, error) {
		return staging, nil
	}
	c.forcePromoteStaging = func(zvec.Store) error {
		rec.add("staging:promote")
		return nil
	}
	c.Embed = &batchFailEmbedder{dims: 4, failAfter: 0}

	if _, err := c.Start(true); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	if p := c.CurrentProgress(); p.State != StateError {
		t.Fatalf("expected failed force reindex, got %+v", p)
	}
	// The failed staging build is not published, so it is not compacted.
	if n := rec.count("staging:compact running=true"); n != 0 {
		t.Fatalf("failed staging build was compacted %d times; events=%v", n, rec.list())
	}
	// The active collection keeps serving and received the journal replay.
	if n := rec.count("active:compact running=true dirty=true"); n != 1 {
		t.Fatalf("active journal-replay changes were not finalized: %d compactions; events=%v",
			n, rec.list())
	}
}

// A successful force run replaces the active collection, so compacting it would be
// pure waste: the rename throws it away.
func TestCoordinatorSuccessfulForceReindexDoesNotCompactActive(t *testing.T) {
	rec := &compactRecorder{}
	active := newCompactableZvec(rec, "active")
	c, _ := compactionTestWorkspace(t, rec, active)

	journal, err := manifest.OpenCleanupJournal(c.Settings.IndexDir)
	if err != nil {
		t.Fatalf("open cleanup journal: %v", err)
	}
	if err := journal.Append([]string{"ghost-doc"}); err != nil {
		t.Fatalf("append journal: %v", err)
	}

	staging := newCompactableZvec(rec, "staging")
	c.forceStagingOpen = func(zvec.Config) (zvec.Store, error) {
		return staging, nil
	}
	c.forcePromoteStaging = func(s zvec.Store) error {
		rec.add("staging:promote")
		st, ok := s.(*compactableZvec)
		if !ok {
			return fmt.Errorf("unexpected staging store %T", s)
		}
		st.mu.Lock()
		copied := make(map[string]zvec.Chunk, len(st.chunks))
		for k, v := range st.chunks {
			copied[k] = v
		}
		st.mu.Unlock()
		active.mu.Lock()
		active.chunks = copied
		active.mu.Unlock()
		return zvec.PromoteStagingManifest(c.Settings.IndexDir)
	}

	if _, err := c.Start(true); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	if p := c.CurrentProgress(); p.State != StateIdle {
		t.Fatalf("final progress=%+v", p)
	}
	if n := rec.count("active:compact running=true"); n != 0 {
		t.Fatalf("successful force run compacted the collection it discards: %d; events=%v",
			n, rec.list())
	}
	if n := rec.count("staging:compact running=true dirty=true"); n != 1 {
		t.Fatalf("staging build was not finalized before promote: %d; events=%v", n, rec.list())
	}
}

// Shutdown must not start a new expensive rebuild: a cancelled run skips final
// maintenance even though it may have changed the collection.
func TestCoordinatorCancelledRunSkipsCompaction(t *testing.T) {
	rec := &compactRecorder{}
	active := newCompactableZvec(rec, "active")
	c, _ := compactionTestWorkspace(t, rec, active)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.SetLifecycleContext(ctx)

	if _, err := c.Start(false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	if p := c.CurrentProgress(); p.State != StateIdle || p.Message != InterruptedMessage {
		t.Fatalf("expected an interrupted run, got %+v", p)
	}
	if n := rec.count("active:compact running=true"); n != 0 {
		t.Fatalf("cancelled run compacted %d times; events=%v", n, rec.list())
	}
}

// The run must not publish Running=false before maintenance ends; a caller polling
// IsRunning must never see a quiescent index that is still being rebuilt.
func TestCoordinatorCompactionCompletesBeforeRunningFlips(t *testing.T) {
	rec := &compactRecorder{}
	active := newCompactableZvec(rec, "active")
	c, _ := compactionTestWorkspace(t, rec, active)

	if _, err := c.Start(false); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for c.IsRunning() {
		if time.Now().After(deadline) {
			t.Fatal("run did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// By the time IsRunning is false the maintenance event must already be recorded.
	if n := rec.count("active:compact running=true dirty=true"); n != 1 {
		t.Fatalf("maintenance missing after the run flipped to not-running; events=%v", rec.list())
	}
}

// A run that fails after partial changes still gets best-effort maintenance, and the
// original error survives: the maintenance pass must not mask what actually failed.
//
// The fixture needs a real partial write, so a second file is added and the embedder
// fails on the second batch: the first file is indexed (dirty) before the failure.
func TestCoordinatorFailedRunStillFinalizesAndKeepsError(t *testing.T) {
	rec := &compactRecorder{}
	active := newCompactableZvec(rec, "active")
	c, root := compactionTestWorkspace(t, rec, active)
	if err := os.WriteFile(filepath.Join(root, "pkg", "second.go"),
		[]byte("package pkg\n\nfunc Second() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.Embed = &batchFailEmbedder{dims: 4, failAfter: 2}

	if _, err := c.Start(false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	p := c.CurrentProgress()
	if p.State != StateError || p.Error == "" {
		t.Fatalf("expected the embed failure to be reported, got %+v", p)
	}
	// The first file must really have been written, otherwise "partial changes" is a lie.
	if n, err := active.DocCount(); err != nil || n == 0 {
		t.Fatalf("no partial write happened before the failure: n=%d err=%v", n, err)
	}
	if n := rec.count("active:compact running=true dirty=true"); n != 1 {
		t.Fatalf("failed run compacted %d times, want 1; events=%v", n, rec.list())
	}
}

// A maintenance failure on a failed run must not replace the run's error either.
func TestCoordinatorFailedRunKeepsErrorWhenCompactionAlsoFails(t *testing.T) {
	rec := &compactRecorder{}
	rec.setErr(errors.New("native optimize failed"))
	active := newCompactableZvec(rec, "active")
	c, _ := compactionTestWorkspace(t, rec, active)
	c.Embed = &batchFailEmbedder{dims: 4, failAfter: 0}

	if _, err := c.Start(false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCoordinatorIdle(t, c)

	p := c.CurrentProgress()
	if p.State != StateError {
		t.Fatalf("expected error state, got %+v", p)
	}
	if strings.Contains(p.Error, "native optimize failed") {
		t.Fatalf("maintenance error leaked into the run result: %q", p.Error)
	}
	if !strings.Contains(p.Error, "embed") && !strings.Contains(p.Error, "batch") {
		t.Fatalf("run error does not describe the embed failure: %q", p.Error)
	}
}
