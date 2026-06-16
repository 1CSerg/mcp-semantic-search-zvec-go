package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReclaimOrphanedFile(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "LOCK")
	if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !ReclaimOrphanedFile(lockPath) {
		t.Fatal("expected orphaned lock reclaim")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file still exists: %v", err)
	}
}

func TestReclaimOrphanedFileMissing(t *testing.T) {
	if ReclaimOrphanedFile(filepath.Join(t.TempDir(), "missing.lock")) {
		t.Fatal("expected false for missing file")
	}
}
