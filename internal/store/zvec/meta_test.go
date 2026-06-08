package zvec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureIndexMeta(dir, "ws1", "/proj", "smoke", 128); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.WorkspaceID != "ws1" || meta.EmbeddingDimensions != 128 {
		t.Fatalf("meta=%+v", meta)
	}
	if err := ValidateIndexMeta(dir, "ws2", "smoke", 128); err == nil {
		t.Fatal("expected owner mismatch")
	}
}

func TestIndexMetaPathPresent(t *testing.T) {
	dir := t.TempDir()
	if IndexMetaPresent(dir) {
		t.Fatal("expected missing")
	}
	_ = os.WriteFile(filepath.Join(dir, "index_meta.json"), []byte("{}"), 0o644)
	if !IndexMetaPresent(dir) {
		t.Fatal("expected present")
	}
}

func TestValidateIndexMetaDimensions(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureIndexMeta(dir, "ws1", "/proj", "smoke", 128); err != nil {
		t.Fatal(err)
	}
	if err := ValidateIndexMeta(dir, "ws1", "smoke", 64); err == nil {
		t.Fatal("expected dimension mismatch")
	}
	if err := ValidateIndexMeta(dir, "ws1", "other", 128); err == nil {
		t.Fatal("expected profile mismatch")
	}
}
