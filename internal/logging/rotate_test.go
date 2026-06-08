package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriterDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	w, err := newRotatingWriter(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRotatingWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	w, err := newRotatingWriter(path, 64, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	payload := strings.Repeat("x", 40)
	for i := 0; i < 4; i++ {
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("missing log: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("missing rotated log: %v", err)
	}
}
