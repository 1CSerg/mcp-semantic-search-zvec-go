//go:build integration && zvec && windows

package zvec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIntegrationVaultDriveCyrillicIndexDir(t *testing.T) {
	vault := `g:\Мой диск\База знаний`
	if _, err := os.Stat(vault); err != nil {
		t.Skip("vault path not available:", err)
	}

	indexDir := filepath.Join(vault, ".mcp-semantic-search-zvec-go", "data", "index-integration-test")
	if err := os.RemoveAll(indexDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(indexDir) })

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
