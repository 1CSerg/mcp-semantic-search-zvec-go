package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const smokeWorkspaceConfig = `active_profile: smoke
profiles:
  smoke:
    provider: openai_compatible
    model: mock
    base_url: http://127.0.0.1:9/v1
    dimensions: 128
file_watcher:
  enabled: false
`

func writeWorkspaceConfig(t *testing.T, root string) {
	t.Helper()
	install := filepath.Join(root, ".mcp-semantic-search-zvec-go")
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "config.yaml"), []byte(smokeWorkspaceConfig), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryOpenWorkspace(t *testing.T) {
	dir := t.TempDir()
	rootA := filepath.Join(dir, "a")
	rootB := filepath.Join(dir, "b")
	for _, root := range []string{rootA, rootB} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		writeWorkspaceConfig(t, root)
	}

	cfg := Config{
		MaxOpenWorkspaces: 1,
		Workspaces: []WorkspaceSpec{
			{ID: "ws-a", Root: rootA},
			{ID: "ws-b", Root: rootB},
		},
	}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(cfg, t.Context())
	defer r.Close()

	if _, err := r.GetService("ws-a"); err != nil {
		t.Fatalf("open ws-a: %v", err)
	}
	list := r.ListWorkspaces()
	openCount := 0
	for _, ws := range list {
		if ws.Open {
			openCount++
		}
	}
	if openCount != 1 {
		t.Fatalf("open count=%d list=%+v", openCount, list)
	}

	if _, err := r.GetService("ws-b"); err != nil {
		t.Fatalf("open ws-b: %v", err)
	}
	list = r.ListWorkspaces()
	openCount = 0
	var openIDs []string
	for _, ws := range list {
		if ws.Open {
			openCount++
			openIDs = append(openIDs, ws.ID)
		}
	}
	if openCount != 1 {
		t.Fatalf("expected LRU to keep one open, got %d (%v)", openCount, openIDs)
	}
}

func TestRegistryGetServiceRequiresID(t *testing.T) {
	r := NewRegistry(Config{Workspaces: []WorkspaceSpec{{ID: "x", Root: t.TempDir()}}}, t.Context())
	defer r.Close()
	if _, err := r.GetService(""); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err=%v", err)
	}
}

func TestRegistryCloseAll(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceConfig(t, root)
	cfg := Config{Workspaces: []WorkspaceSpec{{ID: "ws", Root: root}}}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())
	if _, err := r.GetService("ws"); err != nil {
		t.Fatal(err)
	}
	r.Close()
	list := r.ListWorkspaces()
	for _, ws := range list {
		if ws.Open {
			t.Fatalf("workspace still open after Close: %+v", ws)
		}
	}
}
