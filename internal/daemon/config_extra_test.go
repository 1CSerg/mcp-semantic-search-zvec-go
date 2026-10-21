package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigMissingPath pins the user-level daemon startup contract: a
// missing daemon.yaml is not an error, because the daemon starts before any
// project is added. It must produce a valid config with an empty registry.
func TestLoadConfigMissingPath(t *testing.T) {
	t.Setenv("MCP_GLOBAL_DIR", t.TempDir())
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("missing daemon config must not be fatal: %v", err)
	}
	if len(cfg.Workspaces) != 0 {
		t.Fatalf("workspaces=%v", cfg.Workspaces)
	}
	if cfg.MaxOpenWorkspaces != defaultMaxOpenWorkspaces {
		t.Fatalf("max_open_workspaces=%d", cfg.MaxOpenWorkspaces)
	}
}

// TestLoadConfigEmptyWorkspaces: an explicitly empty registry is valid so the
// daemon can run before the first project is registered.
func TestLoadConfigEmptyWorkspaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MCP_GLOBAL_DIR", dir)
	path := dir + "/daemon.yaml"
	if err := os.WriteFile(path, []byte("workspaces: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("empty workspaces must be valid: %v", err)
	}
	if len(cfg.Workspaces) != 0 {
		t.Fatalf("workspaces=%v", cfg.Workspaces)
	}
}

func TestRegistryConfig(t *testing.T) {
	cfg := Config{MaxOpenWorkspaces: 3, Workspaces: []WorkspaceSpec{{ID: "x", Root: t.TempDir()}}}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())
	defer r.Close()
	if got := r.Config().MaxOpenWorkspaces; got != 3 {
		t.Fatalf("max=%d", got)
	}
}

func TestAbsPathIfRelative(t *testing.T) {
	base := t.TempDir()
	abs, err := absPathIfRelative("subdir/index", base)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "subdir", "index")
	if abs != want {
		t.Fatalf("abs=%q want=%q", abs, want)
	}
	absBase := filepath.Join(base, "abs")
	if err := os.MkdirAll(absBase, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := absPathIfRelative(absBase, base)
	if err != nil {
		t.Fatal(err)
	}
	if got != absBase {
		t.Fatalf("got=%q want=%q", got, absBase)
	}
}
