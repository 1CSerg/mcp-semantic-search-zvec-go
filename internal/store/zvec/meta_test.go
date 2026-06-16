package zvec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
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

func TestReadIndexMetaMissing(t *testing.T) {
	_, err := ReadIndexMeta(t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureIndexMetaExisting(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureIndexMeta(dir, "ws1", "/proj", "smoke", 128); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexMeta(dir, "ws1", "/proj", "smoke", 128); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.WorkspaceID != "ws1" {
		t.Fatalf("meta=%+v", meta)
	}
}

func TestEnsureIndexMetaSetsZvecGoVersion(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureIndexMeta(dir, "ws1", "/proj", "smoke", 128); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ZvecGoVersion != version.ZvecGoVersion {
		t.Fatalf("ZvecGoVersion=%q want %q", meta.ZvecGoVersion, version.ZvecGoVersion)
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

func TestEnsureIndexMetaBackfillsIncomplete(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{ZvecGoVersion: version.ZvecGoVersion}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexMeta(dir, "ws1", "/proj", "smoke", 128); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := indexMetaFromIdentity(IndexIdentity{
		WorkspaceID:   "ws1",
		WorkspaceRoot: "/proj",
		Profile:       "smoke",
		Dimensions:    128,
	}, version.ZvecGoVersion)
	if meta.WorkspaceID != want.WorkspaceID ||
		meta.WorkspaceRoot != want.WorkspaceRoot ||
		meta.CollectionName != want.CollectionName ||
		meta.ZvecGoVersion != want.ZvecGoVersion {
		t.Fatalf("meta=%+v want=%+v", meta, want)
	}
}
