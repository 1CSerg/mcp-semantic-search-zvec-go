package zvec

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/testutil"
)

// tempDir returns a per-test directory removed with a Windows-aware retry (see
// testutil.RemoveDirWithRetry): zvec closes its native handles asynchronously
// enough that a just-released file can still hold its directory entry for a few
// milliseconds, which otherwise surfaces as an opaque
// "TempDir RemoveAll cleanup: The directory is not empty".
func tempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() { testutil.RemoveDirWithRetry(t, dir) })
	return dir
}
