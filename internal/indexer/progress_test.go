package indexer

import (
	"os"
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
