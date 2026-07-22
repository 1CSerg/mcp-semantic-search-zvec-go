package zvec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPromoteAndDiscardStagingCollection(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{IndexDir: dir, WorkspaceRoot: filepath.Join(dir, "ws"), ProfileName: "test", Dimensions: 4}
	active := CollectionPath(cfg)
	stagingCfg := cfg
	stagingCfg.CollectionSuffix = StagingCollectionSuffix
	staging := CollectionPath(stagingCfg)

	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(active, "active.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "staging.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := PromoteStagingCollection(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(active, "staging.txt")); err != nil {
		t.Fatalf("promoted content missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(active, "active.txt")); !os.IsNotExist(err) {
		t.Fatal("old active content should be gone after promote")
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("staging path should be gone after promote")
	}

	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := DiscardStagingCollection(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("staging path should be gone after discard")
	}
}

func TestPromoteAndDiscardStagingManifest(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "manifest.db")
	staging := StagingManifestPath(dir)
	if err := os.WriteFile(active, []byte("old-manifest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staging, []byte("new-manifest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PromoteStagingManifest(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new-manifest" {
		t.Fatalf("active=%q", data)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("staging manifest should be gone")
	}
	if err := os.WriteFile(staging, []byte("tmp"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DiscardStagingManifest(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("discard should remove staging manifest")
	}
}
