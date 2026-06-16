package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParsePathContainmentMode(t *testing.T) {
	tests := []struct {
		in   string
		want PathContainmentMode
	}{
		{"strict", PathContainmentStrict},
		{"STRICT", PathContainmentStrict},
		{"off", PathContainmentOff},
		{"", PathContainmentWarn},
		{"unknown", PathContainmentWarn},
	}
	for _, tc := range tests {
		if got := ParsePathContainmentMode(tc.in); got != tc.want {
			t.Fatalf("ParsePathContainmentMode(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsPathUnderRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(filepath.Join(root, "data", "index"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(root, "data", "index")
	if !IsPathUnderRoot(root, index) {
		t.Fatalf("expected %q under %q", index, root)
	}
	if !IsPathUnderRoot(root, root) {
		t.Fatal("root should be under itself")
	}
	evil := filepath.Join(filepath.Dir(root), "proj-evil", "data")
	if IsPathUnderRoot(root, evil) {
		t.Fatalf("prefix trap: %q should not be under %q", evil, root)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if IsPathUnderRoot(root, outside) {
		t.Fatalf("%q should not be under %q", outside, root)
	}
}

func TestIsPathUnderRootEscape(t *testing.T) {
	root := t.TempDir()
	escaped := filepath.Join(root, "..", "escaped")
	if IsPathUnderRoot(root, escaped) {
		t.Fatalf("escaped path %q should not be under %q", escaped, root)
	}
}

func TestIsPathAllowedWithAllowlist(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if IsPathAllowed(external, []string{root}, nil) {
		t.Fatal("external path should fail without allowlist")
	}
	if !IsPathAllowed(external, []string{root}, []string{external}) {
		t.Fatal("external path should pass with allowlist")
	}
	child := filepath.Join(external, "nested")
	if !IsPathAllowed(child, []string{root}, []string{external}) {
		t.Fatal("child of allowlisted path should pass")
	}
}

func TestValidatePathContainmentStrict(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	err := ValidatePathContainment(PathContainmentOptions{
		Mode:         PathContainmentStrict,
		FieldName:    "INDEX_DIR",
		Path:         outside,
		AllowedRoots: []string{root},
	})
	if err == nil {
		t.Fatal("expected strict error")
	}
}

func TestValidatePathContainmentWarn(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := ValidatePathContainment(PathContainmentOptions{
		Mode:         PathContainmentWarn,
		FieldName:    "INDEX_DIR",
		Path:         outside,
		AllowedRoots: []string{root},
	}); err != nil {
		t.Fatalf("warn mode should not return error: %v", err)
	}
}

func TestValidatePathContainmentOff(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := ValidatePathContainment(PathContainmentOptions{
		Mode:         PathContainmentOff,
		FieldName:    "INDEX_DIR",
		Path:         outside,
		AllowedRoots: []string{root},
	}); err != nil {
		t.Fatalf("off mode should not return error: %v", err)
	}
}

func TestAbsPaths(t *testing.T) {
	dir := t.TempDir()
	abs, err := AbsPaths([]string{dir, ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(abs) != 1 {
		t.Fatalf("abs=%v", abs)
	}
	want, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if abs[0] != want {
		t.Fatalf("abs[0]=%q want %q", abs[0], want)
	}
}

func TestIsPathUnderRootWindowsDriveCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	root := `D:\Projects\MyApp`
	path := `d:\projects\myapp\data\index`
	if !IsPathUnderRoot(root, path) {
		t.Fatalf("%q should be under %q (case-insensitive)", path, root)
	}
}
