//go:build integration && zvec && windows

package zvec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIntegrationCyrillicIndexDir(t *testing.T) {
	parent := filepath.Join(os.TempDir(), "zvec-тест-индекс-mcp")
	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent) })

	indexDir := filepath.Join(parent, "data", "index")
	cfg := testConfig(t, indexDir)
	store := New(cfg).(*CollectionStore)

	chunks, vectors := seedChunks(10)
	if err := store.UpsertChunks(chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	hits, err := store.Search(unitVector(0, testDims), 5, "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search returned no hits")
	}
	for i, h := range hits {
		if h.Path == "" {
			t.Fatalf("hit[%d] empty path", i)
		}
		if h.Snippet == "" {
			t.Fatalf("hit[%d] empty snippet", i)
		}
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
