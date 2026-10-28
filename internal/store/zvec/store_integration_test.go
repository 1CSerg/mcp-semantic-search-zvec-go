//go:build integration && zvec

package zvec

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const testDims = 8

func testConfig(t *testing.T, indexDir string) Config {
	t.Helper()
	return Config{
		IndexDir:      indexDir,
		WorkspaceRoot: tempDir(t),
		ProfileName:   "integration",
		Dimensions:    testDims,
	}
}

func unitVector(i, dims int) []float32 {
	v := make([]float32, dims)
	if i < dims {
		v[i] = 1
	}
	return v
}

func seedChunks(n int) ([]Chunk, [][]float32) {
	chunks := make([]Chunk, n)
	vectors := make([][]float32, n)
	for i := 0; i < n; i++ {
		chunks[i] = Chunk{
			DocID:        fmt.Sprintf("doc-%03d", i),
			RelativePath: fmt.Sprintf("pkg/file_%d.go", i%10),
			StartLine:    int64(i * 10),
			EndLine:      int64(i*10 + 5),
			ChunkType:    "code",
			Name:         fmt.Sprintf("fn%d", i),
			Snippet:      fmt.Sprintf("snippet %d", i),
		}
		vectors[i] = unitVector(i%testDims, testDims)
	}
	return chunks, vectors
}

func TestIntegrationSpikeChecklist(t *testing.T) {
	indexDir := tempDir(t)
	cfg := testConfig(t, indexDir)
	store := New(cfg).(*CollectionStore)

	// 1-2: create collection + insert 100 docs
	chunks, vectors := seedChunks(100)
	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}
	count, err := store.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 100 {
		t.Fatalf("doc_count=%d want 100", count)
	}

	// 3: vector query ordered by score
	hits, err := store.Search(unitVector(0, testDims), 5, "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search returned no hits")
	}
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Fatalf("scores not descending: %f > %f at %d", hits[i].Score, hits[i-1].Score, i)
		}
	}

	// 4: idempotent Open
	if err := store.Open(); err != nil {
		t.Fatalf("second Open: %v", err)
	}

	// 5: delete by id
	if err := store.DeleteByIDs([]string{"doc-000", "doc-001"}); err != nil {
		t.Fatalf("DeleteByIDs: %v", err)
	}
	count, err = store.DocCount()
	if err != nil {
		t.Fatalf("DocCount after delete: %v", err)
	}
	if count != 98 {
		t.Fatalf("doc_count after delete=%d want 98", count)
	}

	// 6: Close then reopen
	path := CollectionPath(cfg)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	store2 := New(cfg).(*CollectionStore)
	if err := store2.Open(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	count, err = store2.DocCount()
	if err != nil {
		t.Fatalf("DocCount after reopen: %v", err)
	}
	if count != 98 {
		t.Fatalf("doc_count after reopen=%d want 98", count)
	}

	// 7: stale lock reclaim
	if err := store2.Close(); err != nil {
		t.Fatalf("Close before lock test: %v", err)
	}
	lockPath := filepath.Join(path, "LOCK")
	if err := os.WriteFile(lockPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	store3 := New(cfg).(*CollectionStore)
	if err := store3.Open(); err != nil {
		t.Fatalf("open after stale lock: %v", err)
	}

	// path_glob filter
	filtered, err := store3.Search(unitVector(0, testDims), 10, "pkg/file_0.go")
	if err != nil {
		t.Fatalf("Search with glob: %v", err)
	}
	for _, h := range filtered {
		if !matchPathGlob(h.Path, "pkg/file_0.go") {
			t.Fatalf("unexpected path %q", h.Path)
		}
	}

	// Windows: release mmap handles before t.TempDir cleanup.
	if err := store3.Close(); err != nil {
		t.Fatalf("Close store3: %v", err)
	}
}

func TestIntegrationOpenReadOnlyThenWrite(t *testing.T) {
	indexDir := tempDir(t)
	cfg := testConfig(t, indexDir)
	store := New(cfg).(*CollectionStore)

	chunks, vectors := seedChunks(3)
	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks seed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := store.Open(); err != nil {
		t.Fatalf("Open read-only: %v", err)
	}
	if !store.IsOpen() {
		t.Fatal("expected open after Open()")
	}

	hits, err := store.Search(unitVector(0, testDims), 1, "")
	if err != nil {
		t.Fatalf("Search read-only: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search returned no hits")
	}

	extra := []Chunk{{
		DocID:        "doc-extra",
		RelativePath: "pkg/extra.go",
		StartLine:    1,
		EndLine:      2,
		ChunkType:    "code",
		Name:         "Extra",
		Snippet:      "func Extra() {}",
	}}
	extraVec := [][]float32{unitVector(1, testDims)}
	if err := store.UpsertChunks(extra, extraVec); err != nil {
		t.Fatalf("UpsertChunks after read-only Open: %v", err)
	}

	if err := store.DeleteByIDs([]string{"doc-000"}); err != nil {
		t.Fatalf("DeleteByIDs after read-only Open: %v", err)
	}

	count, err := store.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 3 {
		t.Fatalf("doc_count=%d want 3", count)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestIntegrationSearchReturnsSymbolFields(t *testing.T) {
	indexDir := tempDir(t)
	cfg := testConfig(t, indexDir)
	store := New(cfg).(*CollectionStore)

	chunks := []Chunk{{
		DocID:         "sym-doc",
		RelativePath:  "pkg/auth.go",
		StartLine:     1,
		EndLine:       5,
		ChunkType:     "code",
		Name:          "Auth",
		Snippet:       "func Auth() {}",
		SymbolName:    "Auth",
		SymbolKind:    "function",
		ParentScope:   "package pkg",
		ChunkStrategy: "hybrid",
	}}
	vectors := [][]float32{unitVector(0, testDims)}
	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	hits, err := store.Search(unitVector(0, testDims), 1, "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits=%d want 1", len(hits))
	}
	h := hits[0]
	if h.SymbolName != "Auth" || h.SymbolKind != "function" || h.ParentScope != "package pkg" || h.ChunkStrategy != "hybrid" {
		t.Fatalf("symbol fields missing in search hit: %+v", h)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestIntegrationPathGlobExpansion(t *testing.T) {
	indexDir := tempDir(t)
	cfg := testConfig(t, indexDir)
	store := New(cfg).(*CollectionStore)

	chunks := make([]Chunk, 0, 121)
	vectors := make([][]float32, 0, 121)
	for i := 0; i < 120; i++ {
		chunks = append(chunks, Chunk{
			DocID:        fmt.Sprintf("other-%03d", i),
			RelativePath: fmt.Sprintf("other/near_%d.go", i),
			StartLine:    1,
			EndLine:      2,
			ChunkType:    "code",
			Snippet:      fmt.Sprintf("other %d", i),
		})
		vectors = append(vectors, unitVector(0, testDims))
	}
	chunks = append(chunks, Chunk{
		DocID:        "target-match",
		RelativePath: "target/match.go",
		StartLine:    1,
		EndLine:      2,
		ChunkType:    "code",
		Snippet:      "target match",
	})
	vectors = append(vectors, unitVector(testDims-1, testDims))

	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	hits, err := store.Search(unitVector(0, testDims), 1, "target/*.go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits=%d want 1", len(hits))
	}
	if hits[0].Path != "target/match.go" {
		t.Fatalf("path=%q want target/match.go", hits[0].Path)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestIntegrationPathGlobExpansionTopKAboveDocCount covers the second broken
// shape: with limit greater than the collection size the expansion loop never
// ran a query and returned zero hits, even though the only match was present.
func TestIntegrationPathGlobExpansionTopKAboveDocCount(t *testing.T) {
	indexDir := tempDir(t)
	cfg := testConfig(t, indexDir)
	store := New(cfg).(*CollectionStore)

	chunks := []Chunk{
		{
			DocID:        "other-doc",
			RelativePath: "other/near.go",
			StartLine:    1,
			EndLine:      2,
			ChunkType:    "code",
			Snippet:      "other",
		},
		{
			DocID:        "target-doc",
			RelativePath: "target/match.go",
			StartLine:    1,
			EndLine:      2,
			ChunkType:    "code",
			Snippet:      "target match",
		},
	}
	// The match has the worst rank, so only a full pass can find it.
	vectors := [][]float32{unitVector(0, testDims), unitVector(testDims-1, testDims)}
	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	hits, err := store.Search(unitVector(0, testDims), 5, "target/*.go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits=%d want 1", len(hits))
	}
	if hits[0].Path != "target/match.go" {
		t.Fatalf("path=%q want target/match.go", hits[0].Path)
	}

	// Unfiltered search must be unaffected by the glob schedule fix.
	all, err := store.Search(unitVector(0, testDims), 5, "")
	if err != nil {
		t.Fatalf("Search without glob: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered hits=%d want 2", len(all))
	}
	if all[0].Path != "other/near.go" {
		t.Fatalf("unfiltered top path=%q want other/near.go", all[0].Path)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestIntegrationPathGlobExpansionFullPass covers a non-matching glob on a
// collection whose size sits between two powers of two: the loop must terminate
// after a full pass and return no hits instead of looping or truncating.
func TestIntegrationPathGlobExpansionFullPass(t *testing.T) {
	indexDir := tempDir(t)
	cfg := testConfig(t, indexDir)
	store := New(cfg).(*CollectionStore)

	const docs = 121
	chunks := make([]Chunk, 0, docs)
	vectors := make([][]float32, 0, docs)
	for i := 0; i < docs; i++ {
		chunks = append(chunks, Chunk{
			DocID:        fmt.Sprintf("doc-%03d", i),
			RelativePath: fmt.Sprintf("other/near_%d.go", i),
			StartLine:    1,
			EndLine:      2,
			ChunkType:    "code",
			Snippet:      fmt.Sprintf("doc %d", i),
		})
		vectors = append(vectors, unitVector(0, testDims))
	}
	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	hits, err := store.Search(unitVector(0, testDims), 3, "target/*.go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("hits=%d want 0", len(hits))
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestIntegrationOpenStagingOnCleanIndex covers the staged force-reindex path:
// the staging collection must be created writable (read-only Open returns
// ErrCollectionMissing on a clean index), and promote must work once the handles
// are released (Windows cannot rename a directory with open files).
func TestIntegrationOpenStagingOnCleanIndex(t *testing.T) {
	indexDir := tempDir(t)
	cfg := testConfig(t, indexDir)
	stagingCfg := cfg
	stagingCfg.CollectionSuffix = StagingCollectionSuffix

	if _, err := os.Stat(CollectionPath(stagingCfg)); !os.IsNotExist(err) {
		t.Fatalf("staging collection must not exist before the build: %v", err)
	}

	store, err := OpenStaging(stagingCfg)
	if err != nil {
		t.Fatalf("OpenStaging: %v", err)
	}
	if _, err := os.Stat(CollectionPath(stagingCfg)); err != nil {
		t.Fatalf("staging collection not created: %v", err)
	}

	chunks, vectors := seedChunks(3)
	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close staging: %v", err)
	}
	if err := PromoteStagingCollection(cfg); err != nil {
		t.Fatalf("PromoteStagingCollection: %v", err)
	}
	if _, err := os.Stat(CollectionPath(stagingCfg)); !os.IsNotExist(err) {
		t.Fatal("staging collection should be gone after promote")
	}

	active := New(cfg).(*CollectionStore)
	n, err := active.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if n != len(chunks) {
		t.Fatalf("doc count=%d want %d", n, len(chunks))
	}
	if err := active.Close(); err != nil {
		t.Fatalf("Close active: %v", err)
	}
}

// seedDeleteFixture creates a collection with the given ids.
func seedDeleteFixture(t *testing.T, ids ...string) (*CollectionStore, Config) {
	t.Helper()
	cfg := testConfig(t, tempDir(t))
	store := New(cfg).(*CollectionStore)
	chunks := make([]Chunk, 0, len(ids))
	vectors := make([][]float32, 0, len(ids))
	for i, id := range ids {
		chunks = append(chunks, Chunk{
			DocID:        id,
			RelativePath: "pkg/del.go",
			StartLine:    1,
			EndLine:      2,
			ChunkType:    "code",
			Snippet:      "snippet " + id,
		})
		vectors = append(vectors, unitVector(i%testDims, testDims))
	}
	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks seed: %v", err)
	}
	return store, cfg
}

// Deleting an id that never existed must not fail: the delete goal is already met,
// and zvec reports that case through ErrorCount.
func TestIntegrationDeleteMissingIDIsNoOp(t *testing.T) {
	store, _ := seedDeleteFixture(t, "doc-a", "doc-b")
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	if err := store.DeleteByIDs([]string{"never-existed"}); err != nil {
		t.Fatalf("DeleteByIDs missing id: %v", err)
	}
	n, err := store.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if n != 2 {
		t.Fatalf("doc count=%d want 2", n)
	}
}

// A repeated delete must succeed after the durability flush, so a cleanup journal
// replay is idempotent.
func TestIntegrationDeleteIsIdempotent(t *testing.T) {
	store, cfg := seedDeleteFixture(t, "doc-a", "doc-b")
	ids := []string{"doc-a", "doc-b"}
	if err := store.DeleteByIDs(ids); err != nil {
		t.Fatalf("first DeleteByIDs: %v", err)
	}
	if err := store.DeleteByIDs(ids); err != nil {
		t.Fatalf("repeat DeleteByIDs must be idempotent: %v", err)
	}
	if n, err := store.DocCount(); err != nil || n != 0 {
		t.Fatalf("doc count=%d err=%v want 0", n, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The repeat delete must also be durable: a reopen still shows an empty index.
	reopened := New(cfg).(*CollectionStore)
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("Close reopened: %v", err)
		}
	}()
	if n, err := reopened.DocCount(); err != nil || n != 0 {
		t.Fatalf("doc count after reopen=%d err=%v want 0", n, err)
	}
}

// A mix of present and absent ids deletes what exists and reports no error.
func TestIntegrationDeleteMixedPresentAndMissing(t *testing.T) {
	store, _ := seedDeleteFixture(t, "doc-a", "doc-b")
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	if err := store.DeleteByIDs([]string{"doc-a", "gone", "doc-b", "also-gone"}); err != nil {
		t.Fatalf("DeleteByIDs mixed: %v", err)
	}
	if n, err := store.DocCount(); err != nil || n != 0 {
		t.Fatalf("doc count=%d err=%v want 0", n, err)
	}
}

// Duplicate ids in one request must not be treated as a partial failure.
func TestIntegrationDeleteDuplicates(t *testing.T) {
	store, _ := seedDeleteFixture(t, "doc-a", "doc-b")
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	if err := store.DeleteByIDs([]string{"doc-a", "doc-a", "doc-b", "doc-a"}); err != nil {
		t.Fatalf("DeleteByIDs with duplicates: %v", err)
	}
	if n, err := store.DocCount(); err != nil || n != 0 {
		t.Fatalf("doc count=%d err=%v want 0", n, err)
	}
}

// DeleteByIDs must never rewrite the caller's slice: the cleanup journal and the
// coordinator reuse the ids after the call.
func TestIntegrationDeleteDoesNotMutateInput(t *testing.T) {
	store, _ := seedDeleteFixture(t, "doc-a", "doc-b")
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	ids := make([]string, 4, 8)
	copy(ids, []string{"doc-a", "gone", "doc-b", "gone"})
	original := append([]string(nil), ids...)

	if err := store.DeleteByIDs(ids); err != nil {
		t.Fatalf("DeleteByIDs: %v", err)
	}
	if len(ids) != len(original) {
		t.Fatalf("input length changed: %d want %d", len(ids), len(original))
	}
	for i := range original {
		if ids[i] != original[i] {
			t.Fatalf("input[%d]=%q want %q (slice was rewritten)", i, ids[i], original[i])
		}
	}
}
