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
	p.FilesDone = 1
	m := p.ToIndexingMap()
	if m["running"] != true {
		t.Fatalf("map=%v", m)
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
	if p.Message != "indexing complete with 2 file errors (see server.log)" {
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
