package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDiscoverGit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	init := exec.Command("git", "init", dir)
	init.Dir = dir
	if out, err := init.CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	add := exec.Command("git", "add", "main.go")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Skipf("git add: %v %s", err, out)
	}

	result, err := Discover(Options{
		Root:       dir,
		Extensions: []string{".go"},
		SkipDirs:   []string{".git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0] != "main.go" {
		t.Fatalf("files=%v", result.Files)
	}
	if result.Method != "git" {
		t.Fatalf("method=%q", result.Method)
	}
}

func TestDiscoverWalk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Discover(Options{
		Root:       dir,
		Extensions: []string{".go"},
		SkipDirs:   []string{".git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0] != "main.go" {
		t.Fatalf("files=%v", result.Files)
	}
	if result.Method != "walk" {
		t.Fatalf("method=%q", result.Method)
	}
}

func TestDiscoverWalkRecordsSkippedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unreadable directory semantics differ on Windows")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o000); err != nil {
		t.Skip("cannot create unreadable dir:", err)
	}
	defer func() { _ = os.Chmod(blocked, 0o755) }()

	result, err := Discover(Options{Root: dir, Extensions: []string{".go"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("files=%v skipped=%v", result.Files, result.SkippedPaths)
	}
	if len(result.SkippedPaths) == 0 {
		t.Fatal("expected skipped paths on unreadable directory")
	}
}

func TestDiscoverEmptyRoot(t *testing.T) {
	if _, err := Discover(Options{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestMatchesWatchPath(t *testing.T) {
	ext := []string{".go"}
	skip := []string{".git", "node_modules"}
	if !MatchesWatchPath("pkg/auth.go", ext, skip) {
		t.Fatal("expected match")
	}
	if MatchesWatchPath("node_modules/x.go", ext, skip) {
		t.Fatal("expected skip")
	}
	if MatchesWatchPath("readme.txt", ext, skip) {
		t.Fatal("expected extension mismatch")
	}
}

func TestShouldSkipPath(t *testing.T) {
	if !shouldSkipPath("vendor/pkg/x.go", []string{"vendor"}) {
		t.Fatal("expected skip")
	}
}

func TestFilterFiles(t *testing.T) {
	files := []string{"a.go", "vendor/x.go", "b.txt", "pkg/main.go"}
	out := filterFiles(files, Options{
		Extensions: []string{".go"},
		SkipDirs:   []string{"vendor"},
	})
	if len(out) != 2 {
		t.Fatalf("out=%v", out)
	}
}

func TestShouldSkipDir(t *testing.T) {
	if !shouldSkipDir("node_modules", []string{"node_modules"}) {
		t.Fatal("expected skip")
	}
}

func TestMatchesExtension(t *testing.T) {
	if !matchesExtension("a.Go", []string{".go"}) {
		t.Fatal("expected case-insensitive ext")
	}
	if !matchesExtension("a.go", []string{}) {
		t.Fatal("empty extensions should match all")
	}
}
