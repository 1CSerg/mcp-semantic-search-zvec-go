package crash

import (
	"encoding/json"
	"os"
	"path/filepath"
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

type fmtError string

func (e fmtError) Error() string { return string(e) }
