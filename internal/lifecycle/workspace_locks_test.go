package lifecycle

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

// globalCollectionSettings builds settings for a projects.d collection: a non-empty
// CollectionID is what marks the global-install layout.
func globalCollectionSettings(t *testing.T, workspace, indexDir, collectionID string) *config.Settings {
	t.Helper()
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(indexDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return &config.Settings{
		WorkspaceRoot: workspace,
		IndexDir:      indexDir,
		CollectionID:  collectionID,
		WorkspaceID:   collectionID,
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible", Model: "m", Dimensions: 8},
			},
			Indexing: config.IndexingConfig{LockStaleSeconds: 60, StallSeconds: 60},
		},
	}
}

// TestPrepareWorkspaceLocksSkipsGlobalCollectionProcessStop is the regression guard
// for the global-install layout: the shared daemon must not scan for and kill
// "competing" stdio processes for a collection it owns, because the MCP proxies of
// that project are its clients. Their command line names the project root (via
// --project-config), so the legacy scan would kill the sessions being served.
func TestPrepareWorkspaceLocksSkipsGlobalCollectionProcessStop(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "project")
	indexDir := filepath.Join(dir, "collections", "col-a")
	settings := globalCollectionSettings(t, workspace, indexDir, "col-a")

	// A stand-in for the MCP proxy: the helper's command line names the workspace.
	helper := startStaleHelper(t, workspace)
	defer func() {
		if helper.Process != nil {
			_ = helper.Process.Kill()
			_, _ = helper.Process.Wait()
		}
	}()

	if err := PrepareWorkspaceLocks(settings); err != nil {
		t.Fatalf("PrepareWorkspaceLocks: %v", err)
	}

	time.Sleep(killGrace + 300*time.Millisecond)
	if !lock.ProcessAlive(helper.Process.Pid) {
		t.Fatal("PrepareWorkspaceLocks killed the MCP proxy of a global collection")
	}
}

// TestPrepareWorkspaceLocksLegacyStillStopsStdio pins that the legacy per-project
// behaviour is preserved: an empty CollectionID means the stdio server owns the
// index, so a competing instance for the same workspace is still stopped.
func TestPrepareWorkspaceLocksLegacyStillStopsStdio(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "project")
	indexDir := filepath.Join(dir, "data", "index")
	settings := globalCollectionSettings(t, workspace, indexDir, "")
	// Legacy: no collection id.
	settings.CollectionID = ""
	settings.WorkspaceID = ""

	helper := startStaleHelper(t, workspace)
	defer func() {
		if helper.Process != nil {
			_ = helper.Process.Kill()
			_, _ = helper.Process.Wait()
		}
	}()

	if err := PrepareWorkspaceLocks(settings); err != nil {
		t.Fatalf("PrepareWorkspaceLocks: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !lock.ProcessAlive(helper.Process.Pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("legacy mode must still stop a competing stdio instance for the same workspace")
}
