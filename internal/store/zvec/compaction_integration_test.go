//go:build integration && zvec

package zvec

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	zvec "github.com/zvec-ai/zvec-go"
)

// The compaction fixture writes many small batches, each followed by the Flush the
// production store performs per upsert. zvec finalizes a memory component and
// allocates a fresh on-disk block on every flush, so an unmaintained index grows
// one .proxima block per batch. These tests measure that growth, prove that a
// native Optimize() reclaims it, and pin the regression expectation for the
// automatic maintenance policy (CollectionStore.compactLocked / Compactor).
const (
	compactionBatches  = 256
	compactionPerBatch = 4
	// compactionShrinkFactor is the minimum reduction in .proxima file count and
	// bytes the maintenance policy must deliver against the unmaintained baseline.
	compactionShrinkFactor = 4
)

// collectionMetrics summarizes the on-disk footprint of a zvec collection.
type collectionMetrics struct {
	proximaFiles int
	proximaBytes int64
	totalBytes   int64
}

func (m collectionMetrics) String() string {
	return fmt.Sprintf("proxima_files=%d proxima_bytes=%d total_bytes=%d",
		m.proximaFiles, m.proximaBytes, m.totalBytes)
}

// measureCollection walks dir and reports the .proxima block count and sizes. A
// missing directory measures as zero: maintenance must never create a collection.
func measureCollection(t *testing.T, dir string) collectionMetrics {
	t.Helper()
	var m collectionMetrics
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return m
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		m.totalBytes += info.Size()
		if strings.HasSuffix(strings.ToLower(d.Name()), ".proxima") {
			m.proximaFiles++
			m.proximaBytes += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("measure collection %s: %v", dir, err)
	}
	return m
}

// logCollectionTree lists the collection directory contents so a failing run
// shows which files make up the footprint.
func logCollectionTree(t *testing.T, dir string) {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = path
		}
		lines = append(lines, fmt.Sprintf("%s=%d", rel, info.Size()))
		return nil
	})
	if err != nil {
		t.Logf("walk %s: %v", dir, err)
		return
	}
	sort.Strings(lines)
	t.Logf("collection tree (%d files): %s", len(lines), strings.Join(lines, ", "))
}

// openRawCollection creates or opens a collection without the CollectionStore
// maintenance policy, so the fixture can build a true unmaintained baseline.
func openRawCollection(t *testing.T, path, name string, dims int) *zvec.Collection {
	t.Helper()
	if err := ensureInit(); err != nil {
		t.Fatalf("zvec initialize: %v", err)
	}
	schema, err := buildSchema(name, dims)
	if err != nil {
		t.Fatalf("build schema: %v", err)
	}
	defer schema.Destroy()

	opts := zvec.NewCollectionOptions()
	if opts == nil {
		t.Fatal("create collection options")
	}
	defer opts.Destroy()
	if err := opts.SetReadOnly(false); err != nil {
		t.Fatalf("SetReadOnly: %v", err)
	}
	if err := opts.SetEnableMmap(false); err != nil {
		t.Fatalf("SetEnableMmap: %v", err)
	}
	col, err := zvec.CreateAndOpen(path, schema, opts)
	if err != nil {
		t.Fatalf("CreateAndOpen: %v", err)
	}
	return col
}

// fixtureBatch returns the deterministic chunks and vectors of one fixture batch.
func fixtureBatch(batch int) ([]Chunk, [][]float32) {
	chunks := make([]Chunk, 0, compactionPerBatch)
	vectors := make([][]float32, 0, compactionPerBatch)
	for i := 0; i < compactionPerBatch; i++ {
		n := batch*compactionPerBatch + i
		chunks = append(chunks, Chunk{
			DocID:        fmt.Sprintf("doc-%05d", n),
			RelativePath: fmt.Sprintf("pkg/file_%d.go", n%10),
			StartLine:    int64(n * 10),
			EndLine:      int64(n*10 + 5),
			ChunkType:    "code",
			Name:         fmt.Sprintf("fn%d", n),
			Snippet:      fmt.Sprintf("snippet %d", n),
		})
		vectors = append(vectors, unitVector(n%testDims, testDims))
	}
	return chunks, vectors
}

// writeRawBatch upserts one batch into a native collection and flushes, mirroring
// CollectionStore.UpsertChunks without any maintenance.
func writeRawBatch(t *testing.T, col *zvec.Collection, batch int) {
	t.Helper()
	chunks, vectors := fixtureBatch(batch)
	docs := make([]*zvec.Doc, 0, len(chunks))
	defer func() {
		for _, d := range docs {
			if d != nil {
				d.Destroy()
			}
		}
	}()
	for i, ch := range chunks {
		doc, err := chunkToDoc(ch, vectors[i])
		if err != nil {
			t.Fatalf("chunkToDoc: %v", err)
		}
		docs = append(docs, doc)
	}
	wr, err := col.Upsert(docs)
	if err != nil {
		t.Fatalf("raw upsert batch %d: %v", batch, err)
	}
	if wr.ErrorCount > 0 {
		t.Fatalf("raw upsert batch %d: %d errors", batch, wr.ErrorCount)
	}
	if err := col.Flush(); err != nil {
		t.Fatalf("raw flush batch %d: %v", batch, err)
	}
}

// writeStoreBatches feeds the same fixture through the production store.
func writeStoreBatches(t *testing.T, store *CollectionStore) {
	t.Helper()
	for batch := 0; batch < compactionBatches; batch++ {
		chunks, vectors := fixtureBatch(batch)
		if err := store.UpsertChunks(chunks, vectors); err != nil {
			t.Fatalf("UpsertChunks batch %d: %v", batch, err)
		}
	}
}

// buildUnmaintainedBaseline creates a raw collection with one flush per batch.
//
// The handle is released by a deferred close as well as the explicit one: a mid-test
// t.Fatalf on Windows would otherwise leave the native handle open, and the
// RemoveDirWithRetry cleanup would then fail too, masking the real assertion.
func buildUnmaintainedBaseline(t *testing.T, dims int) (dir string, m collectionMetrics) {
	t.Helper()
	dir = tempDir(t)
	path := filepath.Join(dir, "zvec", "baseline")
	col := openRawCollection(t, path, "baseline", dims)
	closed := false
	defer func() {
		if !closed {
			_ = col.Close()
		}
	}()
	for batch := 0; batch < compactionBatches; batch++ {
		writeRawBatch(t, col, batch)
	}
	if err := col.Close(); err != nil {
		t.Fatalf("close baseline: %v", err)
	}
	closed = true
	return dir, measureCollection(t, path)
}

// TestCompactionBaselineGrowth reproduces the block growth on a temporary
// collection and proves a native Optimize() reclaims it without losing documents
// or search results.
func TestCompactionBaselineGrowth(t *testing.T) {
	dir, before := buildUnmaintainedBaseline(t, testDims)
	path := filepath.Join(dir, "zvec", "baseline")
	logCollectionTree(t, path)
	t.Logf("baseline after %d batches (no maintenance): %s", compactionBatches, before)

	if before.proximaFiles < 2 {
		t.Fatalf("expected block growth from %d flushes, got %s", compactionBatches, before)
	}

	col := openRawCollectionExisting(t, path)
	defer func() {
		if err := col.Close(); err != nil {
			t.Fatalf("close baseline: %v", err)
		}
	}()
	if err := col.Optimize(); err != nil {
		t.Fatalf("native Optimize: %v", err)
	}
	after := measureCollection(t, path)
	logCollectionTree(t, path)
	t.Logf("baseline after native Optimize: %s", after)

	if after.proximaFiles*compactionShrinkFactor > before.proximaFiles {
		t.Fatalf("Optimize did not reduce .proxima count by %dx: before %s, after %s",
			compactionShrinkFactor, before, after)
	}
	if after.proximaBytes*compactionShrinkFactor > before.proximaBytes {
		t.Fatalf("Optimize did not reduce .proxima bytes by %dx: before %s, after %s",
			compactionShrinkFactor, before, after)
	}
	if after.totalBytes > before.totalBytes {
		t.Fatalf("Optimize grew the collection: before %s, after %s", before, after)
	}

	// Documents and search must survive the maintenance pass.
	stats, err := col.GetStats()
	if err != nil {
		t.Fatalf("GetStats after Optimize: %v", err)
	}
	wantDocs := int64(compactionBatches * compactionPerBatch)
	if int64(stats.DocCount) != wantDocs {
		t.Fatalf("doc count after Optimize=%d want %d", stats.DocCount, wantDocs)
	}
	assertAllFixtureIDsPresent(t, col)
	assertSearchWorks(t, col)
}

// openRawCollectionExisting opens an existing collection without the store policy.
func openRawCollectionExisting(t *testing.T, path string) *zvec.Collection {
	t.Helper()
	if err := ensureInit(); err != nil {
		t.Fatalf("zvec initialize: %v", err)
	}
	opts := zvec.NewCollectionOptions()
	if opts == nil {
		t.Fatal("create collection options")
	}
	defer opts.Destroy()
	if err := opts.SetReadOnly(false); err != nil {
		t.Fatalf("SetReadOnly: %v", err)
	}
	if err := opts.SetEnableMmap(false); err != nil {
		t.Fatalf("SetEnableMmap: %v", err)
	}
	col, err := zvec.Open(path, opts)
	if err != nil {
		t.Fatalf("open collection %s: %v", path, err)
	}
	return col
}

// assertAllFixtureIDsPresent fetches every fixture document by primary key.
func assertAllFixtureIDsPresent(t *testing.T, col *zvec.Collection) {
	t.Helper()
	ids := make([]string, 0, compactionBatches*compactionPerBatch)
	for n := 0; n < compactionBatches*compactionPerBatch; n++ {
		ids = append(ids, fmt.Sprintf("doc-%05d", n))
	}
	const batchSize = 128
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		docs, err := col.Fetch(ids[start:end], &zvec.FetchOptions{IncludeVector: false})
		if err != nil {
			t.Fatalf("fetch ids[%d:%d]: %v", start, end, err)
		}
		found := 0
		for _, doc := range docs {
			if doc != nil {
				found++
				doc.Destroy()
			}
		}
		if found != end-start {
			t.Fatalf("fetch ids[%d:%d] found %d docs, want %d", start, end, found, end-start)
		}
	}
}

// assertSearchWorks runs a vector query and requires a non-empty ordered result.
func assertSearchWorks(t *testing.T, col *zvec.Collection) {
	t.Helper()
	q := zvec.NewSearchQuery()
	defer q.Destroy()
	if err := q.SetFieldName(fieldEmbedding); err != nil {
		t.Fatalf("SetFieldName: %v", err)
	}
	if err := q.SetTopK(5); err != nil {
		t.Fatalf("SetTopK: %v", err)
	}
	if err := q.SetOutputFields(searchOutputFields); err != nil {
		t.Fatalf("SetOutputFields: %v", err)
	}
	if err := q.SetQueryVector(unitVector(0, testDims)); err != nil {
		t.Fatalf("SetQueryVector: %v", err)
	}
	results, err := col.Query(q)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer zvec.FreeDocs(results)
	if len(results) == 0 {
		t.Fatal("Query returned no hits after Optimize")
	}
	for i := 1; i < len(results); i++ {
		if results[i].GetScore() > results[i-1].GetScore() {
			t.Fatalf("scores not descending at %d", i)
		}
	}
}

// TestCompactionAutomaticMaintenance is the regression expectation for the
// automatic maintenance policy: after the same fixture the store's collection must
// hold at least compactionShrinkFactor times fewer .proxima blocks and bytes than
// the unmaintained baseline, with documents and search intact.
//
// RED until CollectionStore compaction (Compactor.Compact) is implemented.
func TestCompactionAutomaticMaintenance(t *testing.T) {
	_, baseline := buildUnmaintainedBaseline(t, testDims)
	t.Logf("unmaintained baseline: %s", baseline)

	indexDir := tempDir(t)
	cfg := testConfig(t, indexDir)
	store := New(cfg).(*CollectionStore)
	writeStoreBatches(t, store)
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	path := CollectionPath(cfg)
	after := measureCollection(t, path)
	logCollectionTree(t, path)
	t.Logf("store after %d batches (policy enabled): %s", compactionBatches, after)

	count, err := store.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != compactionBatches*compactionPerBatch {
		t.Fatalf("doc count=%d want %d", count, compactionBatches*compactionPerBatch)
	}

	if after.proximaFiles*compactionShrinkFactor > baseline.proximaFiles {
		t.Fatalf(".proxima count not reduced %dx: baseline %s, store %s",
			compactionShrinkFactor, baseline, after)
	}
	if after.proximaBytes*compactionShrinkFactor > baseline.proximaBytes {
		t.Fatalf(".proxima bytes not reduced %dx: baseline %s, store %s",
			compactionShrinkFactor, baseline, after)
	}
}

// TestCompactionCloseReopenAfterOptimize keeps the durability contract of the
// baseline: a collection that was optimized stays readable after close/reopen.
func TestCompactionCloseReopenAfterOptimize(t *testing.T) {
	dir, before := buildUnmaintainedBaseline(t, testDims)
	path := filepath.Join(dir, "zvec", "baseline")

	col := openRawCollectionExisting(t, path)
	if err := col.Optimize(); err != nil {
		t.Fatalf("native Optimize: %v", err)
	}
	if err := col.Close(); err != nil {
		t.Fatalf("close after Optimize: %v", err)
	}

	reopened := openRawCollectionExisting(t, path)
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("close reopened: %v", err)
		}
	}()
	stats, err := reopened.GetStats()
	if err != nil {
		t.Fatalf("GetStats after reopen: %v", err)
	}
	if int64(stats.DocCount) != int64(compactionBatches*compactionPerBatch) {
		t.Fatalf("doc count after reopen=%d want %d", stats.DocCount, compactionBatches*compactionPerBatch)
	}
	assertAllFixtureIDsPresent(t, reopened)
	assertSearchWorks(t, reopened)
	after := measureCollection(t, path)
	t.Logf("before Optimize: %s; after close/reopen: %s", before, after)
}

// TestCompactionCleanCollectionOptimizeIsCheap documents that Optimize on a
// collection with no pending writes is a cheap no-op at the file level. It is the
// native evidence behind "unchanged runs must not call Optimize".
func TestCompactionCleanCollectionOptimizeIsCheap(t *testing.T) {
	dir, _ := buildUnmaintainedBaseline(t, testDims)
	path := filepath.Join(dir, "zvec", "baseline")

	col := openRawCollectionExisting(t, path)
	if err := col.Optimize(); err != nil {
		t.Fatalf("first Optimize: %v", err)
	}
	settled := measureCollection(t, path)
	if err := col.Optimize(); err != nil {
		t.Fatalf("second Optimize: %v", err)
	}
	again := measureCollection(t, path)
	if err := col.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	t.Logf("settled: %s; after second Optimize: %s", settled, again)
	if again.proximaFiles > settled.proximaFiles {
		t.Fatalf("second Optimize added blocks: %s -> %s", settled, again)
	}
}

func TestCompactionMeasureMissingDirIsZero(t *testing.T) {
	dir := filepath.Join(tempDir(t), "absent")
	m := measureCollection(t, dir)
	if m.proximaFiles != 0 || m.proximaBytes != 0 || m.totalBytes != 0 {
		t.Fatalf("missing dir metrics=%s want zero", m)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("measureCollection must not create %s", dir)
	}
}

// TestCompactionOptimizeFailureDoesNotFailWrite pins the durability contract on a
// real collection: a maintenance failure is advisory and must not turn a durable
// upsert into a reported error, and the collection must stay marked dirty so a
// later pass retries.
func TestCompactionOptimizeFailureDoesNotFailWrite(t *testing.T) {
	indexDir := tempDir(t)
	cfg := testConfig(t, indexDir)
	store := New(cfg).(*CollectionStore)
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	chunks, vectors := fixtureBatch(0)
	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}

	optimizeErr := errors.New("native optimize failed")
	store.mu.Lock()
	store.compactHook = func() error { return optimizeErr }
	store.writeOps = compactionWriteThreshold - 1
	store.mu.Unlock()

	next, nextVectors := fixtureBatch(1)
	if err := store.UpsertChunks(next, nextVectors); err != nil {
		t.Fatalf("upsert must not report the maintenance failure: %v", err)
	}

	store.mu.Lock()
	dirty := store.dirty
	ops := store.writeOps
	store.compactHook = nil
	store.mu.Unlock()
	if !dirty {
		t.Fatal("a failed maintenance pass must leave the collection dirty")
	}
	if ops != 0 {
		t.Fatalf("writeOps=%d want 0 after a failed pass restarts the interval", ops)
	}

	// The documents written before the failed pass are durable and visible.
	count, err := store.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 2*compactionPerBatch {
		t.Fatalf("doc count=%d want %d", count, 2*compactionPerBatch)
	}

	// A later successful pass still reclaims the blocks.
	if err := store.Compact(); err != nil {
		t.Fatalf("Compact after clearing the failure: %v", err)
	}
	store.mu.Lock()
	dirty = store.dirty
	store.mu.Unlock()
	if dirty {
		t.Fatal("a successful pass must clear the dirty flag")
	}
}

// TestCompactionNativeOptimizeOnReadOnlyHandleFails documents the native contract the
// read-only guard in compactLocked depends on: zvec refuses to rewrite the index through
// a read-only handle. promoteStaging reopens the active collection with Open()
// (read-only), so without the guard an unchanged run would call this every start and
// never clear the dirty flag.
func TestCompactionNativeOptimizeOnReadOnlyHandleFails(t *testing.T) {
	indexDir := tempDir(t)
	cfg := testConfig(t, indexDir)
	store := New(cfg).(*CollectionStore)
	chunks, vectors := seedChunks(8)
	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen the way promoteStaging does: read-only.
	if err := store.Open(); err != nil {
		t.Fatalf("Open read-only: %v", err)
	}
	store.mu.Lock()
	readOnly := store.readOnly
	store.mu.Unlock()
	if !readOnly {
		t.Fatal("Open() must open read-only; the guard premise is wrong")
	}

	optimizeErr := store.col.Optimize()
	t.Logf("native Optimize on a read-only handle: %v", optimizeErr)
	if optimizeErr == nil {
		t.Fatal("zvec accepted Optimize on a read-only handle; the read-only guard is dead code")
	}

	// The guard turns that native failure into a silent deferral, keeping dirty.
	store.mu.Lock()
	store.dirty = true
	store.mu.Unlock()
	if err := store.Compact(); err != nil {
		t.Fatalf("Compact on a read-only handle must not report the native failure: %v", err)
	}
	store.mu.Lock()
	dirty := store.dirty
	store.mu.Unlock()
	if !dirty {
		t.Fatal("dirty must survive a deferred read-only pass")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
