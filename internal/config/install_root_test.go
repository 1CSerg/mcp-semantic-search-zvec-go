package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadWorkspaceRootMarker(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "project")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, WorkspaceRootMarkerFile)
	if err := os.WriteFile(marker, []byte(want+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadWorkspaceRootMarker(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveWorkspaceRootPrefersEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORKSPACE_ROOT", dir)
	got := ResolveWorkspaceRoot(os.Getenv("WORKSPACE_ROOT"))
	if got != mustAbs(dir) {
		t.Fatalf("got %q want %q", got, mustAbs(dir))
	}
}

func TestReadWorkspaceRootMarkerUnicode(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "Тест установки семант поиска")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, WorkspaceRootMarkerFile)
	if err := os.WriteFile(marker, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadWorkspaceRootMarker(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestReadWorkspaceRootMarkerMissing(t *testing.T) {
	_, err := ReadWorkspaceRootMarker(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing marker")
	}
}

func TestReadWorkspaceRootMarkerEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, WorkspaceRootMarkerFile), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadWorkspaceRootMarker(dir)
	if err == nil {
		t.Fatal("expected error for empty marker")
	}
}

func TestResolveWorkspaceRootCwdFallback(t *testing.T) {
	got := ResolveWorkspaceRoot("")
	if got == "" {
		t.Fatal("expected non-empty cwd fallback")
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("got %q want absolute path", got)
	}
}
