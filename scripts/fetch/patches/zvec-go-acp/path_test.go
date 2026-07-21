package zvec

import (
	"path/filepath"
	"testing"
)

func TestNormalizeFilesystemPathEmpty(t *testing.T) {
	got, err := normalizeFilesystemPath("")
	if err != nil || got != "" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestNormalizeFilesystemPathAbsolute(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested", "index")
	got, err := normalizeFilesystemPath(sub)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
}
