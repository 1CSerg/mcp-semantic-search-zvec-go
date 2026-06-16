package chunk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAndChunkRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"../outside.go", "../../etc/passwd", "a/../../escape.go"} {
		if _, err := ReadAndChunk(root, rel, Options{}); err == nil {
			t.Fatalf("expected escape rejected for %q", rel)
		}
	}
}

func TestReadAndChunkWithinRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nfunc A() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chunks, err := ReadAndChunk(root, "a.go", Options{})
	if err != nil {
		t.Fatalf("ReadAndChunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
}
