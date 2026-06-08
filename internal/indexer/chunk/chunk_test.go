package chunk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileChunksStableIDs(t *testing.T) {
	content := []byte("line1\nline2\nline3\nline4\nline5\n")
	chunks := FileChunks("pkg/a.go", content, Options{WindowLines: 3, OverlapLines: 1})
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	if chunks[0].DocID == "" || chunks[0].RelativePath != "pkg/a.go" {
		t.Fatalf("chunk=%+v", chunks[0])
	}
	again := FileChunks("pkg/a.go", content, Options{WindowLines: 3, OverlapLines: 1})
	if again[0].DocID != chunks[0].DocID {
		t.Fatalf("doc id not stable: %s vs %s", again[0].DocID, chunks[0].DocID)
	}
}

func TestChunkTypeForPath(t *testing.T) {
	if chunkTypeForPath("a.md") != "markdown" {
		t.Fatal("md")
	}
	if chunkTypeForPath("a.go") != "code" {
		t.Fatal("go")
	}
}

func TestFileChunksDefaultsAndEdges(t *testing.T) {
	if got := FileChunks("a.go", nil, Options{}); len(got) != 0 {
		t.Fatalf("nil content: %v", got)
	}
	if got := FileChunks("a.go", []byte("\n\n\n"), Options{WindowLines: 2, OverlapLines: 2}); len(got) != 0 {
		t.Fatalf("whitespace only: %v", got)
	}
	chunks := FileChunks("cfg.yaml", []byte("k: v\n"), Options{})
	if len(chunks) != 1 || chunks[0].ChunkType != "config" {
		t.Fatalf("chunks=%+v", chunks)
	}
	if chunkTypeForPath("x.json") != "config" || chunkTypeForPath("x.toml") != "config" {
		t.Fatal("expected config type")
	}
}

func TestReadAndChunkMissingFile(t *testing.T) {
	_, err := ReadAndChunk(t.TempDir(), "missing.go", Options{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadAndChunk(t *testing.T) {
	root := t.TempDir()
	rel := "pkg/a.go"
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("package pkg\n\nfunc Auth() {}\n")
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := ReadAndChunk(root, rel, Options{WindowLines: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
}
