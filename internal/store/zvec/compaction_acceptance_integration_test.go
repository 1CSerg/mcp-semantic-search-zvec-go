//go:build integration && zvec

package zvec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingOptimizer wraps the store's real native Optimize so a test can count
// maintenance passes without changing what they do.
//
// The hook runs with s.mu already held, so it must not take the mutex again. It reads
// s.col directly and tolerates a nil handle, because compactLocked deliberately allows
// the hook to run with no handle (a Close racing between two Compact calls).
func countingOptimizer(store *CollectionStore) *int64 {
	var calls int64
	store.mu.Lock()
	store.compactHook = func() error {
		if store.col == nil {
			return nil
		}
		atomic.AddInt64(&calls, 1)
		return store.col.Optimize()
	}
	store.mu.Unlock()
	return &calls
}

// An unchanged run must not call the native optimizer at all: an explicit final
// Compact on a clean collection is a no-op, and repeated finals stay no-ops.
func TestCompactionOptimizeCallCountUnchangedRunIsZero(t *testing.T) {
	cfg := testConfig(t, tempDir(t))
	store := New(cfg).(*CollectionStore)
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	calls := countingOptimizer(store)

	// Two batches: far below the threshold, then the final pass.
	for batch := 0; batch < 2; batch++ {
		chunks, vectors := fixtureBatch(batch)
		if err := store.UpsertChunks(chunks, vectors); err != nil {
			t.Fatalf("UpsertChunks: %v", err)
		}
	}
	if err := store.Compact(); err != nil {
		t.Fatalf("final Compact: %v", err)
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("optimize calls after a dirty run with a final pass=%d want 1", got)
	}

	// Nothing changed since: a second final pass and an unchanged "run" do nothing.
	if err := store.Compact(); err != nil {
		t.Fatalf("Compact on clean collection: %v", err)
	}
	if err := store.Compact(); err != nil {
		t.Fatalf("second Compact on clean collection: %v", err)
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("unchanged collection triggered %d optimize calls in total, want 1", got)
	}
}

// A long run must not optimize on every batch: the periodic passes are bounded by the
// threshold, and the run's final pass adds at most one more attempt.
func TestCompactionOptimizeCallCountIsBounded(t *testing.T) {
	cfg := testConfig(t, tempDir(t))
	store := New(cfg).(*CollectionStore)
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	calls := countingOptimizer(store)

	// 200 batches = 1 periodic pass at 128 + 72 leftover operations.
	const batches = 200
	for batch := 0; batch < batches; batch++ {
		chunks, vectors := fixtureBatch(batch)
		if err := store.UpsertChunks(chunks, vectors); err != nil {
			t.Fatalf("UpsertChunks batch %d: %v", batch, err)
		}
	}
	periodic := int64(batches / compactionWriteThreshold)
	if got := atomic.LoadInt64(calls); got != periodic {
		t.Fatalf("periodic optimize calls=%d want %d", got, periodic)
	}

	// The final pass is the single extra attempt the plan allows.
	if err := store.Compact(); err != nil {
		t.Fatalf("final Compact: %v", err)
	}
	if got := atomic.LoadInt64(calls); got != periodic+1 {
		t.Fatalf("optimize calls after the final pass=%d want %d", got, periodic+1)
	}

	// The final pass cleared dirty, so nothing more happens.
	if err := store.Compact(); err != nil {
		t.Fatalf("Compact after final: %v", err)
	}
	if got := atomic.LoadInt64(calls); got != periodic+1 {
		t.Fatalf("extra optimize calls after a clean final pass: %d want %d", got, periodic+1)
	}
}

// Search, Compact and Close share one native handle: concurrent use must neither
// crash nor deadlock, and a search issued after Close must return an error instead of
// touching a released handle.
func TestCompactionConcurrentSearchCompactClose(t *testing.T) {
	// Unicode path: the plan calls it out explicitly, and Windows handles it
	// differently from ASCII paths.
	root := filepath.Join(tempDir(t), "проект-тест", "индекс")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir unicode index dir: %v", err)
	}
	cfg := Config{
		IndexDir:      root,
		WorkspaceRoot: tempDir(t),
		ProfileName:   "integration",
		Dimensions:    testDims,
	}
	store := New(cfg).(*CollectionStore)

	chunks, vectors := seedChunks(64)
	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var successfulSearches, searchErrs int64
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				hits, err := store.Search(unitVector(0, testDims), 3, "")
				if err != nil {
					// After Close an error is the correct answer; a crash is not.
					atomic.AddInt64(&searchErrs, 1)
					return
				}
				if len(hits) == 0 {
					t.Error("concurrent Search returned no hits while the collection was populated")
					return
				}
				atomic.AddInt64(&successfulSearches, 1)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 8; i++ {
			if err := store.Compact(); err != nil && !errors.Is(err, ErrCollectionMissing) {
				t.Errorf("Compact: %v", err)
				return
			}
		}
	}()

	// Give the readers time to overlap with the maintenance passes.
	time.Sleep(150 * time.Millisecond)
	close(stop)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wg.Wait()

	if atomic.LoadInt64(&successfulSearches) == 0 {
		t.Fatal("no search completed successfully while maintenance was running")
	}
	t.Logf("concurrent searches: %d ok, %d errored after Close",
		atomic.LoadInt64(&successfulSearches), atomic.LoadInt64(&searchErrs))

	// Search opens the collection lazily, so after Close it either reopens cleanly or
	// reports an error — what it must never do is read the released handle. Either
	// outcome is accepted; a crash or a corrupt read is not.
	if hits, err := store.Search(unitVector(0, testDims), 1, ""); err == nil {
		if len(hits) != 1 {
			t.Fatalf("Search after Close returned %d hits, want 1", len(hits))
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close after the post-close search: %v", err)
	}
	// The collection must still be usable through a fresh store: nothing was corrupted.
	reopened := New(cfg).(*CollectionStore)
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("Close reopened: %v", err)
		}
	}()
	n, err := reopened.DocCount()
	if err != nil {
		t.Fatalf("DocCount after reopen: %v", err)
	}
	if n != len(chunks) {
		t.Fatalf("doc count after reopen=%d want %d", n, len(chunks))
	}
}

// TestCompactionFootprintAndLatency records the numbers the plan asks for: wall time
// of a maintained run, peak collection size, and search latency while an Optimize is
// in flight. It asserts only the properties that must hold; the measurements are
// logged for the plan's results section.
func TestCompactionFootprintAndLatency(t *testing.T) {
	cfg := testConfig(t, tempDir(t))
	store := New(cfg).(*CollectionStore)
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()
	path := CollectionPath(cfg)

	var peak int64
	start := time.Now()
	for batch := 0; batch < compactionBatches; batch++ {
		chunks, vectors := fixtureBatch(batch)
		if err := store.UpsertChunks(chunks, vectors); err != nil {
			t.Fatalf("UpsertChunks batch %d: %v", batch, err)
		}
		if batch%32 == 31 {
			if m := measureCollection(t, path); m.totalBytes > peak {
				peak = m.totalBytes
			}
		}
	}
	elapsed := time.Since(start)
	if err := store.Compact(); err != nil {
		t.Fatalf("final Compact: %v", err)
	}
	final := measureCollection(t, path)
	t.Logf("maintained run: wall=%s peak_total_bytes=%d final=%s", elapsed, peak, final)
	if final.totalBytes > peak {
		t.Fatalf("final size %d exceeds sampled peak %d", final.totalBytes, peak)
	}

	// Search latency while a full Optimize runs on the same collection.
	var maxLatency time.Duration
	var samples int
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			t0 := time.Now()
			if _, err := store.Search(unitVector(0, testDims), 5, ""); err != nil {
				return
			}
			if d := time.Since(t0); d > maxLatency {
				maxLatency = d
			}
			samples++
		}
	}()

	// Dirty the collection so the optimizer actually rebuilds.
	extra, extraVectors := fixtureBatch(compactionBatches)
	if err := store.UpsertChunks(extra, extraVectors); err != nil {
		t.Fatalf("UpsertChunks extra: %v", err)
	}
	optStart := time.Now()
	if err := store.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	optElapsed := time.Since(optStart)
	close(stop)
	<-done
	t.Logf("optimize wall=%s; search samples=%d max_latency=%s", optElapsed, samples, maxLatency)

	if optElapsed <= 0 {
		t.Fatal("optimize did not run")
	}
	if samples == 0 {
		t.Fatal("no search completed while the optimizer ran")
	}
}

// A force-reindex staging collection is maintained before promote, so the published
// collection is compacted rather than the raw one-block-per-flush build.
//
// The fixture deliberately stays BELOW compactionWriteThreshold: with 256 batches the
// periodic pass would already have compacted staging twice, and the explicit final
// Compact this test exists for would be a no-op that proves nothing.
func TestCompactionStagingBuildIsMaintainedBeforePromote(t *testing.T) {
	const batches = compactionBatches / 4 // 64 < 128: no periodic pass
	indexDir := tempDir(t)
	cfg := testConfig(t, indexDir)
	stagingCfg := cfg
	stagingCfg.CollectionSuffix = StagingCollectionSuffix

	staging, err := OpenStaging(stagingCfg)
	if err != nil {
		t.Fatalf("OpenStaging: %v", err)
	}
	stagingStore := staging.(*CollectionStore)
	for batch := 0; batch < batches; batch++ {
		chunks, vectors := fixtureBatch(batch)
		if err := stagingStore.UpsertChunks(chunks, vectors); err != nil {
			t.Fatalf("UpsertChunks batch %d: %v", batch, err)
		}
	}
	stagingPath := CollectionPath(stagingCfg)
	grown := measureCollection(t, stagingPath)
	if grown.proximaFiles < 4 {
		t.Fatalf("fixture did not reproduce growth: %s", grown)
	}

	// This is the call the coordinator makes inside promoteStaging, before Close.
	if err := stagingStore.Compact(); err != nil {
		t.Fatalf("final Compact: %v", err)
	}
	before := measureCollection(t, stagingPath)
	t.Logf("staging grown: %s; after Compact: %s", grown, before)
	if before.proximaFiles >= grown.proximaFiles {
		t.Fatalf("explicit Compact did not reclaim blocks: %s -> %s", grown, before)
	}
	if before.proximaFiles > 2 {
		t.Fatalf("explicit Compact left %d blocks, want <= 2: %s", before.proximaFiles, before)
	}

	if err := stagingStore.Close(); err != nil {
		t.Fatalf("Close staging: %v", err)
	}
	if err := PromoteStagingCollection(cfg); err != nil {
		t.Fatalf("PromoteStagingCollection: %v", err)
	}

	promoted := measureCollection(t, CollectionPath(cfg))
	t.Logf("staging before promote: %s; promoted: %s", before, promoted)
	if promoted.proximaFiles != before.proximaFiles {
		t.Fatalf("promote changed the block count: %s -> %s", before, promoted)
	}

	active := New(cfg).(*CollectionStore)
	defer func() {
		if err := active.Close(); err != nil {
			t.Fatalf("Close active: %v", err)
		}
	}()
	n, err := active.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if n != batches*compactionPerBatch {
		t.Fatalf("doc count=%d want %d", n, batches*compactionPerBatch)
	}
}

// The fixture IDs and DocCount must be identical with and without maintenance, which
// is what makes the policy a pure footprint change.
func TestCompactionMaintenancePreservesEveryDocument(t *testing.T) {
	cfg := testConfig(t, tempDir(t))
	store := New(cfg).(*CollectionStore)
	writeStoreBatches(t, store)
	if err := store.Compact(); err != nil {
		t.Fatalf("final Compact: %v", err)
	}

	ids := make([]string, 0, compactionBatches*compactionPerBatch)
	for n := 0; n < compactionBatches*compactionPerBatch; n++ {
		ids = append(ids, fmt.Sprintf("doc-%05d", n))
	}
	present, err := store.DocIDsPresent(ids)
	if err != nil {
		t.Fatalf("DocIDsPresent: %v", err)
	}
	if !present {
		t.Fatal("maintenance lost documents")
	}
	hits, err := store.Search(unitVector(0, testDims), 5, "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search returned no hits after maintenance")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
