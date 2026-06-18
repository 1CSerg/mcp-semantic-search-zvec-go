package chunk

import (
	"os"
	"path/filepath"
	"strings"
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

func TestReadAndChunkRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(outsideFile, []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(root, "link.go")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skip("symlinks not supported:", err)
	}
	_, err := ReadAndChunk(root, "link.go", Options{})
	if err == nil {
		t.Fatal("expected symlink escape rejected")
	}
	if !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("err=%v", err)
	}
}
