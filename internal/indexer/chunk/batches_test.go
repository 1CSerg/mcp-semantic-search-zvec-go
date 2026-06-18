package chunk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestProcessBatches(t *testing.T) {
	root := t.TempDir()
	rel := "pkg/a.go"
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("package pkg\n\nfunc Auth() {}\n\nfunc Other() {}\n")
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}

	var batches int
	var total int
	n, err := ProcessBatches(root, rel, Options{WindowLines: 2, ChunkingStrategy: "line_window"}, &token.HeuristicCounter{}, 1, func(batch []zvec.Chunk) error {
		batches++
		total += len(batch)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 || batches == 0 {
		t.Fatalf("n=%d batches=%d", n, batches)
	}
	if total != n {
		t.Fatalf("total=%d n=%d", total, n)
	}
}
