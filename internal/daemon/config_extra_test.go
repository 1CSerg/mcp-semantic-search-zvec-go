package daemon

import (
	"os"
	"testing"
)

func TestLoadConfigMissingPath(t *testing.T) {
	if _, err := LoadConfig(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadConfigEmptyWorkspaces(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/daemon.yaml"
	if err := os.WriteFile(path, []byte("workspaces: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected empty workspaces error")
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
