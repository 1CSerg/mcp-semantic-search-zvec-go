//go:build realworld && zvec

package harness

import (
	"os"
	"path/filepath"
	"testing"
)

// WriteStaleZvecLock writes a stale LOCK file under INDEX_DIR/zvec for reclaim tests (C4).
func WriteStaleZvecLock(t *testing.T, repo string) string {
	t.Helper()
	indexDir := IndexDir(repo)
	zvecRoot := filepath.Join(indexDir, "zvec")
	var lockPath string
	_ = filepath.WalkDir(zvecRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == "0" {
			lockPath = filepath.Join(path, "LOCK")
		}
		return nil
	})
	if lockPath == "" {
		// No collection yet — create a plausible path for post-reindex reclaim.
		lockPath = filepath.Join(zvecRoot, "ws_placeholder", "0", "LOCK")
		if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
			t.Fatalf("mkdir lock parent: %v", err)
		}
	}
	if err := os.WriteFile(lockPath, []byte("stale-realworld-lock"), 0o644); err != nil {
		t.Fatalf("write stale LOCK: %v", err)
	}
	return lockPath
}

// WriteStdioLock writes a stdio.lock file in the index dir (for cleanup assertions).
func WriteStdioLock(t *testing.T, repo string) {
	t.Helper()
	p := filepath.Join(IndexDir(repo), "stdio.lock")
	if err := os.WriteFile(p, []byte("99999\n"), 0o644); err != nil {
		t.Fatalf("write stdio.lock: %v", err)
	}
}
