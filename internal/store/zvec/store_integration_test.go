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
		WorkspaceRoot: t.TempDir(),
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
	indexDir := t.TempDir()
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
	indexDir := t.TempDir()
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
	indexDir := t.TempDir()
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
