package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "daemon.yaml")
	content := `max_open_workspaces: 2
workspaces:
  - id: app-a
    root: ` + strings.ReplaceAll(root, `\`, `/`) + `
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxOpenWorkspaces != 2 {
		t.Fatalf("max=%d", cfg.MaxOpenWorkspaces)
	}
	if len(cfg.Workspaces) != 1 {
		t.Fatalf("workspaces=%d", len(cfg.Workspaces))
	}
	spec := cfg.Workspaces[0]
	if spec.ID != "app-a" {
		t.Fatalf("id=%q", spec.ID)
	}
	if !filepath.IsAbs(spec.Root) {
		t.Fatalf("root not abs: %q", spec.Root)
	}
	if !strings.Contains(spec.IndexDir, "data") {
		t.Fatalf("index_dir=%q", spec.IndexDir)
	}
}

func TestLoadConfigDuplicateID(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "daemon.yaml")
	content := `workspaces:
  - id: same
    root: ` + root + `
  - id: same
    root: ` + root + `
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(cfgPath); err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestRegistryUnknownWorkspace(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		MaxOpenWorkspaces: 1,
		Workspaces: []WorkspaceSpec{{
			ID:   "known",
			Root: root,
		}},
	}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())
	defer r.Close()

	if _, err := r.GetService("missing"); err == nil {
		t.Fatal("expected error")
	} else if !strings.Contains(err.Error(), "unknown workspace") {
		t.Fatalf("err=%v", err)
	}
}

func TestRegistryListWorkspaces(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Workspaces: []WorkspaceSpec{{ID: "ws1", Root: root}},
	}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())
	defer r.Close()
	list := r.ListWorkspaces()
	if len(list) != 1 || list[0].ID != "ws1" || list[0].Open {
		t.Fatalf("list=%+v", list)
	}
}
