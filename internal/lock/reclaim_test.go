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
	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("lock file should remain after truncate reclaim: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected truncated lock file, size=%d", info.Size())
	}
}

func TestReclaimOrphanedFileMissing(t *testing.T) {
	if ReclaimOrphanedFile(filepath.Join(t.TempDir(), "missing.lock")) {
		t.Fatal("expected false for missing file")
	}
}
