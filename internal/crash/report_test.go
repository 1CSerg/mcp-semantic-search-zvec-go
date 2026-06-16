package crash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteNil(t *testing.T) {
	if err := Write(t.TempDir(), "0.1.0", "/w", nil); err != nil {
		t.Fatal(err)
	}
}

func TestWriteStringFatal(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, "0.2.0", "/w", "panic string"); err != nil {
		t.Fatal(err)
	}
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, "0.1.0", "/workspace", fmtError("boom")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "last_crash.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Error != "boom" {
		t.Fatalf("error=%q", report.Error)
	}
	if report.Version != "0.1.0" {
		t.Fatalf("version=%q", report.Version)
	}
}

func TestWriteMkdirBlocked(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Write(filepath.Join(blocked, "logs"), "0.1.0", "/w", fmtError("boom"))
	if err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestSanitizeStackRedactsRoot(t *testing.T) {
	root := filepath.Join("/Users", "alice", "project")
	stack := "panic at " + root + "/internal/foo.go:42"
	got := SanitizeStack(stack, root)
	if got == stack {
		t.Fatalf("stack not redacted: %q", got)
	}
	if !contains(got, "<redacted>") {
		t.Fatalf("got=%q", got)
	}
}

func TestWriteWithOptionsRedactsWorkspaceRoot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "secret-workspace")
	if err := WriteWithOptions(dir, "0.1.0", fmtError("boom"), WriteOptions{
		RedactPaths:   true,
		WorkspaceRoot: root,
		WorkspaceID:   "ws-1",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "last_crash.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.WorkspaceRoot == root {
		t.Fatalf("workspace root leaked: %q", report.WorkspaceRoot)
	}
	if report.WorkspaceID != "ws-1" {
		t.Fatalf("workspace_id=%q", report.WorkspaceID)
	}
	if contains(report.Stack, root) {
		t.Fatalf("stack leaked path: %q", report.Stack)
	}
}

func TestSanitizeStackUsesStringsContains(t *testing.T) {
	got := SanitizeStack("at /Users/alice/foo.go", "/Users/alice")
	if !strings.Contains(got, "<redacted>") {
		t.Fatalf("got=%q", got)
	}
}

type fmtError string

func (e fmtError) Error() string { return string(e) }

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
