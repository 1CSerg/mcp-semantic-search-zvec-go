//go:build realworld && zvec

package scenarios

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func watcherEnv() []string {
	return []string{
		"FILE_WATCHER_ENABLED=true",
		"FILE_WATCHER_BACKEND=polling",
		"FILE_WATCHER_POLL_INTERVAL_SECONDS=2",
	}
}

func TestIncrementalFileChange(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	idx := harness.TmpIndexDir(t, repo, "incremental-file-change")
	srv := harness.StartHTTPServerWithConfigIndex(t, repo, harness.ConfigPath(repo), idx, 19350, watcherEnv()...)
	harness.ForceReindex(t, srv.HTTPBase)
	harness.AssertIndexReady(t, srv.HTTPBase)

	target := filepath.Join(harness.CorpusDir(repo), "backend", "auth", "middleware.go")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read middleware.go: %v", err)
	}
	marker := "REALWORLD_INCREMENTAL_MARKER_" + time.Now().Format("150405")
	if strings.Contains(string(data), marker) {
		t.Fatal("marker already present")
	}
	newContent := strings.Replace(string(data), "REALWORLD_GO_AUTH_GATE", "REALWORLD_GO_AUTH_GATE\n// "+marker, 1)
	if err := os.WriteFile(target, []byte(newContent), 0o644); err != nil {
		t.Fatalf("write middleware.go: %v", err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(target, data, 0o644)
	})

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		status := harness.GetJSON(t, srv.HTTPBase+"/v1/status")
		idx, _ := status["indexing"].(map[string]any)
		if idx != nil {
			if running, _ := idx["running"].(bool); running {
				time.Sleep(2 * time.Second)
				continue
			}
		}
		resp := harness.PostJSON(t, srv.HTTPBase+"/v1/search", map[string]any{
			"query":      marker,
			"limit":      20,
			"path_glob":  "**/middleware.go",
		})
		results, _ := resp["results"].([]any)
		for _, r := range results {
			item, _ := r.(map[string]any)
			snip, _ := item["snippet"].(string)
			if strings.Contains(snip, marker) {
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("incremental update did not index marker %q", marker)
}

func TestIncrementalDeleteRenameNew(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	idx := harness.TmpIndexDir(t, repo, "incremental-delete-rename")
	srv := harness.StartHTTPServerWithConfigIndex(t, repo, harness.ConfigPath(repo), idx, 19351, watcherEnv()...)
	harness.ForceReindex(t, srv.HTTPBase)
	harness.AssertIndexReady(t, srv.HTTPBase)

	corpus := harness.CorpusDir(repo)
	oldPath := filepath.Join(corpus, "docs", "notes.txt")
	newPath := filepath.Join(corpus, "docs", "notes_renamed.txt")
	orig, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("read notes.txt: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(newPath)
		_ = os.WriteFile(oldPath, orig, 0o644)
	})

	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("rename notes.txt: %v", err)
	}

	newFile := filepath.Join(corpus, "docs", "incremental_new.txt")
	const newMarker = "REALWORLD_INCREMENTAL_NEW_FILE"
	if err := os.WriteFile(newFile, []byte(newMarker+"\n"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(newFile) })

	deadline := time.Now().Add(5 * time.Minute)
	var foundNew bool
	for time.Now().Before(deadline) {
		status := harness.GetJSON(t, srv.HTTPBase+"/v1/status")
		idx, _ := status["indexing"].(map[string]any)
		if idx != nil {
			if running, _ := idx["running"].(bool); running {
				time.Sleep(2 * time.Second)
				continue
			}
		}
		resp := harness.PostJSON(t, srv.HTTPBase+"/v1/search", map[string]any{
			"query": newMarker,
			"limit": 5,
		})
		results, _ := resp["results"].([]any)
		for _, r := range results {
			item, _ := r.(map[string]any)
			path, _ := item["path"].(string)
			if strings.Contains(path, "incremental_new.txt") {
				foundNew = true
				break
			}
		}
		if foundNew {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !foundNew {
		t.Fatal("new file not indexed after watcher event")
	}

	harness.WaitIndexIdle(t, srv.HTTPBase)
	resp := harness.PostJSON(t, srv.HTTPBase+"/v1/search", map[string]any{
		"query": "REALWORLD_TXT_PARAGRAPH",
		"limit": 10,
	})
	results, _ := resp["results"].([]any)
	for _, r := range results {
		item, _ := r.(map[string]any)
		path, _ := item["path"].(string)
		if strings.HasSuffix(path, "notes.txt") {
			t.Fatalf("stale chunk still present for deleted path: %q", path)
		}
	}
}

func TestInterruptIndexingResume(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	idx := harness.TmpIndexDir(t, repo, "interrupt-kill")
	srv := harness.StartHTTPServerWithConfigIndex(t, repo, harness.ConfigPath(repo), idx, 19352)
	harness.ForceReindex(t, srv.HTTPBase)

	deadline := time.Now().Add(30 * time.Second)
	killed := false
	for time.Now().Before(deadline) {
		status := harness.GetJSON(t, srv.HTTPBase+"/v1/status")
		idx, _ := status["indexing"].(map[string]any)
		if idx != nil {
			if running, _ := idx["running"].(bool); running {
				harness.KillProcess9(t, srv.Cmd)
				killed = true
				break
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !killed {
		harness.KillProcess9(t, srv.Cmd)
	}
	time.Sleep(2 * time.Second)

	srv2 := harness.StartHTTPServerWithConfigIndex(t, repo, harness.ConfigPath(repo), harness.TmpIndexDir(t, repo, "interrupt-resume"), 19353)
	harness.ForceReindex(t, srv2.HTTPBase)
	status := harness.AssertIndexReady(t, srv2.HTTPBase)
	indexing, _ := status["indexing"].(map[string]any)
	if indexing != nil {
		if running, _ := indexing["running"].(bool); running {
			t.Fatal("indexing still running after resume")
		}
	}
}
