package scan

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDiscoverGitExcludesWorktreeDeleted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.go"), []byte("package keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gone.go"), []byte("package gone"), 0o644); err != nil {
		t.Fatal(err)
	}

	init := exec.Command("git", "init", dir)
	init.Dir = dir
	if out, err := init.CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	add := exec.Command("git", "add", "keep.go", "gone.go")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Skipf("git add: %v %s", err, out)
	}
	if err := os.Remove(filepath.Join(dir, "gone.go")); err != nil {
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
	if result.Method != "git" {
		t.Fatalf("method=%q", result.Method)
	}
	if len(result.Files) != 1 || result.Files[0] != "keep.go" {
		t.Fatalf("files=%v", result.Files)
	}
	if len(result.SkippedPaths) != 1 || result.SkippedPaths[0] != "gone.go" {
		t.Fatalf("skipped=%v", result.SkippedPaths)
	}
}

func TestExcludeMissingByStat(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.go")
	if err := os.WriteFile(keep, []byte("package keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []string{"keep.go", "missing.go"}
	kept, skipped := excludeMissingByStat(dir, files)
	if len(kept) != 1 || kept[0] != "keep.go" {
		t.Fatalf("kept=%v", kept)
	}
	if len(skipped) != 1 || skipped[0] != "missing.go" {
		t.Fatalf("skipped=%v", skipped)
	}
}

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

// TestDiscoverGitCyrillicPath ensures non-ASCII (Cyrillic) filenames survive git
// discovery. With git's default core.quotepath=true these would arrive as C-style
// octal escapes and silently never match real files on disk (regression guard
// for the core.quotepath=false + ls-files -z fix).
func TestDiscoverGitCyrillicPath(t *testing.T) {
	dir := t.TempDir()
	const cyrName = "Модули.bsl"
	if err := os.WriteFile(filepath.Join(dir, cyrName), []byte("// 1C BSL"), 0o644); err != nil {
		t.Fatal(err)
	}

	init := exec.Command("git", "init", dir)
	init.Dir = dir
	if out, err := init.CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	// Force the problematic default explicitly so the test is robust against a
	// user/global gitconfig that already sets core.quotepath=false.
	cfg := exec.Command("git", "-C", dir, "config", "core.quotepath", "true")
	if out, err := cfg.CombinedOutput(); err != nil {
		t.Skipf("git config: %v %s", err, out)
	}
	add := exec.Command("git", "-C", dir, "add", cyrName)
	if out, err := add.CombinedOutput(); err != nil {
		t.Skipf("git add: %v %s", err, out)
	}

	result, err := Discover(Options{
		Root:       dir,
		Extensions: []string{".bsl"},
		SkipDirs:   []string{".git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Method != "git" {
		t.Fatalf("method=%q", result.Method)
	}
	if len(result.Files) != 1 || result.Files[0] != cyrName {
		t.Fatalf("files=%v want [%s]", result.Files, cyrName)
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

func TestDiscoverWalkSkipsNestedDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "vendor", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor", "pkg", "dep.go"), []byte("package dep"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := discoverWalk(dir, Options{
		Extensions: []string{".go"},
		SkipDirs:   []string{"vendor"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0] != "main.go" {
		t.Fatalf("files=%v", result.Files)
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
	_, err := Discover(Options{})
	if !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestRelForSkip(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "pkg", "skip.go")
	if got := relForSkip(root, child); got != "pkg/skip.go" {
		t.Fatalf("relForSkip=%q", got)
	}
	if got := relForSkip(root, ""); got != "" {
		t.Fatalf("relForSkip empty=%q", got)
	}
	if got := relForSkip(`C:\a`, `C:\b\c.go`); got == "" {
		t.Fatal("expected non-empty fallback")
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
	// Directory Remove/Rename has no extension — still trigger reindex.
	if !MatchesWatchPath("pkg", ext, skip) {
		t.Fatal("expected directory path match")
	}
	if MatchesWatchPath("node_modules", ext, skip) {
		t.Fatal("expected skipped directory")
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
	if !matchesExtension("main.go", []string{"go"}) {
		t.Fatal("expected extension without dot prefix to match")
	}
}

func TestDiscoverWalkSkipsVendorDir(t *testing.T) {
	dir := t.TempDir()
	vendor := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendor, "lib.go"), []byte("package lib"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := discoverWalk(dir, Options{
		Extensions: []string{".go"},
		SkipDirs:   []string{"vendor"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0] != "main.go" {
		t.Fatalf("files=%v", result.Files)
	}
}

func TestDiscoverEmptyGitRepository(t *testing.T) {
	dir := t.TempDir()
	init := exec.Command("git", "init", dir)
	init.Dir = dir
	if out, err := init.CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	res, err := Discover(Options{Root: dir, Extensions: []string{".go"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "empty_repository") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings=%v", res.Warnings)
	}
}

func TestDiscoverGitUnavailable(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Discover(Options{Root: dir, Extensions: []string{".go"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "git_unavailable") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings=%v", res.Warnings)
	}
}
