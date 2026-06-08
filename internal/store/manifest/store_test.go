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

func TestUpsertDeleteListClear(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	entry := FileEntry{
		RelativePath: "a.go",
		MtimeNs:      100,
		Size:         50,
		ChunkCount:   2,
		DocIDs:       []string{"d1", "d2"},
	}
	if err := store.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("a.go")
	if err != nil {
		t.Fatal(err)
	}
	if got.ChunkCount != 2 || len(got.DocIDs) != 2 {
		t.Fatalf("got=%+v", got)
	}
	entry.ChunkCount = 3
	entry.DocIDs = []string{"d1", "d2", "d3"}
	if err := store.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 || list[0].ChunkCount != 3 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if err := store.Delete("a.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("a.go"); err == nil {
		t.Fatal("expected missing")
	}
	if err := store.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	files, chunks, err := store.Stats()
	if err != nil || files != 0 || chunks != 0 {
		t.Fatalf("stats files=%d chunks=%d err=%v", files, chunks, err)
	}
}
