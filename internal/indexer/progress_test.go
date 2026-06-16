package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProgressStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewProgressStore(dir)
	p := StartRunning(true)
	p.FilesTotal = 10
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Running || got.State != StateRunning {
		t.Fatalf("got=%+v", got)
	}
	p = FinishIdle(p, 10, 42)
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Running || got.ChunksIndexed != 42 {
		t.Fatalf("got=%+v", got)
	}
}

func TestToIndexingMap(t *testing.T) {
	p := StartRunning(false)
	p.FilesTotal = 4
	p.FilesDone = 1
	p.StartedAt = "2026-01-01T00:00:00Z"
	p.UpdatedAt = "2026-01-01T00:00:10Z"
	m := p.ToIndexingMap()
	if m["running"] != true {
		t.Fatalf("map=%v", m)
	}
	if m["percent"] != 25.0 {
		t.Fatalf("percent=%v map=%v", m["percent"], m)
	}
	if m["remaining_seconds"] != 30 {
		t.Fatalf("remaining_seconds=%v map=%v", m["remaining_seconds"], m)
	}
}

func TestFinishError(t *testing.T) {
	p := StartRunning(true)
	p = FinishError(p, os.ErrInvalid)
	if p.State != StateError || p.Running || p.Error == "" {
		t.Fatalf("progress=%+v", p)
	}
	m := p.ToIndexingMap()
	if m["error"] == nil || m["finished_at"] == nil {
		t.Fatalf("map=%v", m)
	}
}

func TestFinishIdleWithWarnings(t *testing.T) {
	p := StartRunning(true)
	p.FilesTotal = 3
	p.FilesDone = 3
	p.ChunksIndexed = 5
	p = FinishIdleWithWarnings(p, 2)
	if p.State != StateIdle || p.Running || p.FilesFailed != 2 {
		t.Fatalf("progress=%+v", p)
	}
	if p.Message != "indexing complete with 2 file errors (see diagnostics.log_file; paths in indexing.failed_files)" {
		t.Fatalf("message=%q", p.Message)
	}
	m := p.ToIndexingMap()
	if m["files_failed"] != 2 {
		t.Fatalf("map=%v", m)
	}
	if _, ok := m["file_errors"]; ok {
		t.Fatalf("file_errors should not be in index_status: map=%v", m)
	}
}

func TestProgressLoadMissingFile(t *testing.T) {
	store := NewProgressStore(t.TempDir())
	p, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if p.Running || p.State != StateIdle {
		t.Fatalf("progress=%+v", p)
	}
}

func TestProgressSaveBlocked(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewProgressStore(filepath.Join(blocked, "index"))
	if err := store.Save(StartRunning(false)); err == nil {
		t.Fatal("expected save error")
	}
}

func TestProgressLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	store := NewProgressStore(dir)
	if err := os.WriteFile(store.path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestAppendFailedFile(t *testing.T) {
	p := StartRunning(true)
	AppendFailedFile(&p, "a.go")
	AppendFailedFile(&p, "a.go")
	AppendFailedFile(&p, "b.go")
	if len(p.FailedFiles) != 2 {
		t.Fatalf("failed_files=%v", p.FailedFiles)
	}
}

func TestFinishIdleWithWarningsFailedFilesInMap(t *testing.T) {
	p := StartRunning(true)
	AppendFailedFile(&p, "bad.mdc")
	p = FinishIdleWithWarnings(p, 1)
	m := p.ToIndexingMap()
	files, ok := m["failed_files"].([]string)
	if !ok || len(files) != 1 || files[0] != "bad.mdc" {
		t.Fatalf("map=%v", m)
	}
}

func TestProgressPercentBounds(t *testing.T) {
	p := Progress{FilesTotal: 3, FilesDone: 4}
	if got := p.Percent(); got != 100 {
		t.Fatalf("Percent()=%v, want 100", got)
	}
	p.FilesDone = -1
	if got := p.Percent(); got != 0 {
		t.Fatalf("Percent()=%v, want 0", got)
	}
}

func TestRemainingSecondsUnavailable(t *testing.T) {
	tests := []Progress{
		{Running: false, FilesTotal: 10, FilesDone: 1, StartedAt: "2026-01-01T00:00:00Z"},
		{Running: true, FilesTotal: 10, FilesDone: 0, StartedAt: "2026-01-01T00:00:00Z"},
		{Running: true, FilesTotal: 10, FilesDone: 10, StartedAt: "2026-01-01T00:00:00Z"},
		{Running: true, FilesTotal: 10, FilesDone: 1, StartedAt: "bad"},
	}
	for _, tt := range tests {
		if got, ok := tt.RemainingSeconds(); ok {
			t.Fatalf("RemainingSeconds()=%v, true for %+v", got, tt)
		}
	}
}
