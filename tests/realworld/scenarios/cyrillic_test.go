//go:build realworld && zvec && windows

package scenarios

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func TestCyrillicCorpusPath(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	cyrillicDir := filepath.Join(harness.CorpusDir(repo), "кириллица")
	if _, err := os.Stat(cyrillicDir); err != nil {
		t.Fatalf("cyrillic corpus missing: %v", err)
	}

	srv := harness.StartHTTPServer(t, repo, 19395)
	harness.ForceReindex(t, srv.HTTPBase)
	status := harness.WaitIndexIdle(t, srv.HTTPBase)
	if n, ok := status["zvec_doc_count"].(float64); !ok || n < 1 {
		t.Fatalf("index with cyrillic paths failed: %v", status)
	}

	hit := harness.AssertSearchHit(t, srv.HTTPBase, "REALWORLD_CYRILLIC_PATH", "модуль.go", "", "ast")
	path, _ := hit["path"].(string)
	if !strings.Contains(path, "кириллица") {
		t.Fatalf("expected cyrillic path segment in hit: %q", path)
	}
}

func TestCyrillicIndexDir(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	idx := filepath.Join(harness.RealworldRoot(repo), "данные", "индекс")
	if err := os.MkdirAll(idx, 0o755); err != nil {
		t.Fatalf("mkdir cyrillic index: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(idx)) })

	env := harness.EnvWithOverrides(repo, map[string]string{
		"INDEX_DIR": idx,
	})
	srv := harness.StartHTTPServerWithEnv(t, repo, 19396, env)
	t.Cleanup(func() { harness.KillProcessGraceful(t, srv.Cmd) })

	harness.ForceReindex(t, srv.HTTPBase)
	status := harness.WaitIndexIdle(t, srv.HTTPBase)
	if n, ok := status["zvec_doc_count"].(float64); !ok || n < 1 {
		t.Fatalf("cyrillic INDEX_DIR reindex failed: %v", status)
	}
	diag, _ := status["diagnostics"].(map[string]any)
	if diag != nil {
		if supported, _ := diag["unicode_paths_supported"].(bool); supported {
			t.Log("unicode_paths_supported=true")
		}
	}
}
