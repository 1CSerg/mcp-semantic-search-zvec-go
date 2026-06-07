package zvec

import (
	"os"
	"testing"
)

func TestCollectionPath(t *testing.T) {
	cfg := Config{
		IndexDir:      "/data/index",
		WorkspaceRoot: "/workspace",
		ProfileName:   "openai_local",
		Dimensions:    1536,
	}
	path := CollectionPath(cfg)
	if path == "" {
		t.Fatal("empty path")
	}
	if got := CollectionName(cfg.WorkspaceRoot, cfg.ProfileName, cfg.Dimensions); got == "" {
		t.Fatal("empty collection name")
	}
}

func TestIndexMetaPresent(t *testing.T) {
	dir := t.TempDir()
	if IndexMetaPresent(dir) {
		t.Fatal("expected false")
	}
	if err := os.WriteFile(IndexMetaPath(dir), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IndexMetaPresent(dir) {
		t.Fatal("expected true")
	}
}

func TestCollectionNameStable(t *testing.T) {
	a := CollectionName("/workspace", "profile", 384)
	b := CollectionName("/workspace", "profile", 384)
	if a != b {
		t.Fatalf("unstable: %q vs %q", a, b)
	}
	if len(a) != 3+16 { // ws_ + 16 hex
		t.Fatalf("name=%q", a)
	}
	c := CollectionName("/other", "profile", 384)
	if a == c {
		t.Fatal("expected different workspace to differ")
	}
}
