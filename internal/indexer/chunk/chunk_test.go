package chunk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileChunksStableIDs(t *testing.T) {
	content := []byte("line1\nline2\nline3\nline4\nline5\n")
	chunks := FileChunks("pkg/a.go", content, Options{WindowLines: 3, OverlapLines: 1, ChunkingStrategy: "line_window"})
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	id1 := DocIDForChunk(&chunks[0], 1)
	if id1 == "" || chunks[0].RelativePath != "pkg/a.go" {
		t.Fatalf("chunk=%+v id=%q", chunks[0], id1)
	}
	again := FileChunks("pkg/a.go", content, Options{WindowLines: 3, OverlapLines: 1, ChunkingStrategy: "line_window"})
	id2 := DocIDForChunk(&again[0], 1)
	if id2 != id1 {
		t.Fatalf("doc id not stable: %s vs %s", id2, id1)
	}
}

func TestChunkTypeForPath(t *testing.T) {
	if chunkTypeForPath("a.md") != "markdown" {
		t.Fatal("md")
	}
	if chunkTypeForPath("a.mdc") != "markdown" {
		t.Fatal("mdc")
	}
	if chunkTypeForPath("a.txt") != "markdown" {
		t.Fatal("txt")
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

func TestFileChunksStripsUTF8BOM(t *testing.T) {
	content := []byte{0xEF, 0xBB, 0xBF, 'p', 'a', 'c', 'k', 'a', 'g', 'e', ' ', 'm', 'a', 'i', 'n', '\n'}
	chunks := FileChunks("main.go", content, Options{WindowLines: 10})
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	if strings.HasPrefix(chunks[0].Snippet, "\ufeff") {
		t.Fatalf("snippet still has BOM: %q", chunks[0].Snippet)
	}
}

func TestFileChunksNormalizesCRLF(t *testing.T) {
	content := []byte("line1\r\nline2\r\n")
	chunks := FileChunks("a.go", content, Options{WindowLines: 10})
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	if strings.Contains(chunks[0].Snippet, "\r") {
		t.Fatalf("snippet has CR: %q", chunks[0].Snippet)
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

func TestReadAndChunkRejectsLargeFile(t *testing.T) {
	root := t.TempDir()
	rel := "big.go"
	content := make([]byte, 64)
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadAndChunk(root, rel, Options{MaxFileBytes: 32})
	if err == nil {
		t.Fatal("expected file too large error")
	}
	if !strings.Contains(err.Error(), "file too large for indexing") {
		t.Fatalf("err=%v", err)
	}
}
