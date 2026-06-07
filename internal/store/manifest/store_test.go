package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatsEmpty(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "manifest.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	files, chunks, err := store.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if files != 0 || chunks != 0 {
		t.Fatalf("files=%d chunks=%d", files, chunks)
	}
}

func TestGet(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "manifest.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, err = store.Get("missing.go")
	if err == nil {
		t.Fatal("expected error for missing path")
	}

	_, err = store.db.Exec(`
		INSERT INTO file_manifest (relative_path, mtime_ns, size, chunk_count, doc_ids)
		VALUES (?, ?, ?, ?, ?)
	`, "foo.go", 1, 100, 2, `["id1","id2"]`)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := store.Get("foo.go")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry.RelativePath != "foo.go" || entry.ChunkCount != 2 {
		t.Fatalf("entry=%+v", entry)
	}
}

func TestOpenCreatesFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "manifest.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal(err)
	}
}
