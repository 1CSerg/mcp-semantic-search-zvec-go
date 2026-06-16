package daemon

import (
	"os"
	"path/filepath"
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
