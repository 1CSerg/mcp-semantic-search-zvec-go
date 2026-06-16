package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateWorkspaceRootMarkerValid(t *testing.T) {
	dir := t.TempDir()
	install := filepath.Join(dir, DefaultInstallDirName)
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "config.yaml"), []byte("active_profile: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWorkspaceRootMarker(dir); err != nil {
		t.Fatal(err)
	}
}

func TestValidateWorkspaceRootMarkerMissingInstall(t *testing.T) {
	dir := t.TempDir()
	if err := ValidateWorkspaceRootMarker(dir); err == nil {
		t.Fatal("expected error for missing install dir")
	}
}

func TestReadWorkspaceRootMarkerValidated(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, DefaultInstallDirName)
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(install, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, WorkspaceRootMarkerFile), []byte(root+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadWorkspaceRootMarkerValidated(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("got=%q want=%q", got, root)
	}
}

func TestReadWorkspaceRootMarkerValidatedRejectsInvalid(t *testing.T) {
	binDir := t.TempDir()
	invalidRoot := filepath.Join(t.TempDir(), "no-install")
	if err := os.WriteFile(filepath.Join(binDir, WorkspaceRootMarkerFile), []byte(invalidRoot+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadWorkspaceRootMarkerValidated(binDir); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestResolveWorkspaceRootPrefersEnv(t *testing.T) {
	dir := t.TempDir()
	if got := ResolveWorkspaceRoot(dir); got != mustAbs(dir) {
		t.Fatalf("got=%q", got)
	}
}

func TestReadWorkspaceRootMarker(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, DefaultInstallDirName)
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(install, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, WorkspaceRootMarkerFile), []byte(root+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadWorkspaceRootMarker(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("got=%q want=%q", got, root)
	}
}

func TestWorkspaceRootFromExecutable(t *testing.T) {
	if _, err := WorkspaceRootFromExecutable(); err == nil {
		// Most test runs have no workspace-root.txt next to the test binary.
		t.Skip("workspace-root.txt present next to test binary")
	}
}

func TestResolveWorkspaceRootUsesWorkingDirectory(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveWorkspaceRoot(""); got != mustAbs(wd) {
		t.Fatalf("got=%q want %q", got, mustAbs(wd))
	}
}
