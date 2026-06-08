package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDiscoverWalk(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "a.go"), []byte("package internal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "x", "b.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := Discover(Options{
		Root:       root,
		Extensions: []string{".go", ".md"},
		SkipDirs:   []string{"node_modules", ".git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files=%v", files)
	}
}

func TestMatchesExtension(t *testing.T) {
	if !matchesExtension("a.go", []string{".go"}) {
		t.Fatal("expected match")
	}
	if matchesExtension("a.txt", []string{".go"}) {
		t.Fatal("expected no match")
	}
	if !matchesExtension("a.go", []string{"go"}) {
		t.Fatal("expected match without dot prefix")
	}
}

func TestShouldSkipPath(t *testing.T) {
	if !shouldSkipPath("node_modules/pkg/a.go", []string{"node_modules"}) {
		t.Fatal("expected skip")
	}
	if shouldSkipPath("internal/a.go", []string{"node_modules"}) {
		t.Fatal("expected keep")
	}
}

func TestDiscoverInvalidRoot(t *testing.T) {
	if _, err := Discover(Options{}); err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestDiscoverGit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "a.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "core.autocrlf=false", "-c", "core.safecrlf=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	git("init")
	git("add", "pkg/a.go", "skip.txt")

	files, err := Discover(Options{
		Root:       root,
		Extensions: []string{".go"},
		SkipDirs:   []string{"node_modules"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "pkg/a.go" {
		t.Fatalf("files=%v", files)
	}
}
