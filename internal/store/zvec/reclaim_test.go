package zvec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReclaimCollectionLock(t *testing.T) {
	dir := t.TempDir()
	workspace := t.TempDir()
	cfg := Config{
		IndexDir:      dir,
		WorkspaceRoot: workspace,
		ProfileName:   "test",
		Dimensions:    8,
	}
	collectionPath := CollectionPath(cfg)
	if err := os.MkdirAll(collectionPath, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(collectionPath, "LOCK")
	if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !ReclaimCollectionLock(cfg) {
		t.Fatal("expected reclaim")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock still present: %v", err)
	}
}

func TestReclaimCollectionLockMissingDir(t *testing.T) {
	cfg := Config{
		IndexDir:      t.TempDir(),
		WorkspaceRoot: t.TempDir(),
		ProfileName:   "test",
		Dimensions:    8,
	}
	if ReclaimCollectionLock(cfg) {
		t.Fatal("expected false for missing collection dir")
	}
}

func TestReclaimAllCollectionLocks(t *testing.T) {
	indexDir := t.TempDir()
	for _, name := range []string{"ws_one", "ws_two"} {
		collectionPath := filepath.Join(indexDir, "zvec", name)
		if err := os.MkdirAll(collectionPath, 0o755); err != nil {
			t.Fatal(err)
		}
		lockPath := filepath.Join(collectionPath, "LOCK")
		if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if n := ReclaimAllCollectionLocks(indexDir); n != 2 {
		t.Fatalf("reclaimed=%d want 2", n)
	}
}
