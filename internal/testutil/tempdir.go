package testutil

import (
	"os"
	"testing"
	"time"
)

// RemoveDirWithRetry deletes dir and tolerates the short Windows window in which a
// just-closed file handle still keeps its directory entry alive.
//
// Windows marks a file deleted as pending-delete until the last handle is closed,
// so RemoveAll on the parent can fail with ERROR_DIR_NOT_EMPTY for a few
// milliseconds after the last Close. That is a kernel bookkeeping delay, not a
// resource leak. Anything that survives the whole retry budget is a real leak and
// is reported with the leftover entries so the test fails with evidence instead of
// an opaque "TempDir RemoveAll cleanup: directory is not empty".
//
// Call it from a t.Cleanup registered after t.TempDir(): cleanups run LIFO, so it
// runs before testing's own temp-dir removal.
func RemoveDirWithRetry(t *testing.T, dir string) {
	t.Helper()
	if dir == "" {
		return
	}
	const (
		attempts = 50
		delay    = 10 * time.Millisecond
	)
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if lastErr = os.RemoveAll(dir); lastErr == nil {
			return
		}
		time.Sleep(delay)
	}
	entries, _ := os.ReadDir(dir)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	t.Errorf("directory %q still not removable after %d attempts (leftover entries: %v): %v",
		dir, attempts, names, lastErr)
}
