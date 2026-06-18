package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenInvalidPath(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(filepath.Join(blocked, "manifest.db"))
	if err == nil {
		t.Fatal("expected open error")
	}
}

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

func TestOpenEnablesWALOnLocalPath(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "manifest.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	mode, err := JournalMode(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode=%q", mode)
	}
}

func TestOpenSkipsWALOnSyncedCloudPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "YandexDisk", "project", "data", "index")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANIFEST_WAL", "auto")
	dbPath := filepath.Join(dir, "manifest.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	mode, err := JournalMode(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(mode, "wal") {
		t.Fatalf("expected WAL disabled on synced path, got %q", mode)
	}
}

func TestShouldEnableWALForcedOn(t *testing.T) {
	t.Setenv("MANIFEST_WAL", "on")
	dir := filepath.Join(t.TempDir(), "YandexDisk", "index")
	if !shouldEnableWAL(filepath.Join(dir, "manifest.db")) {
		t.Fatal("expected WAL forced on")
	}
}

func TestShouldEnableWALForcedOff(t *testing.T) {
	t.Setenv("MANIFEST_WAL", "off")
	dir := t.TempDir()
	if shouldEnableWAL(filepath.Join(dir, "manifest.db")) {
		t.Fatal("expected WAL forced off")
	}
}

func TestWalSkipReasonAuto(t *testing.T) {
	t.Setenv("MANIFEST_WAL", "")
	if reason := walSkipReason(filepath.Join(t.TempDir(), "manifest.db")); reason != "auto" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestGetInvalidDocIDsJSON(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.db.Exec(`
		INSERT INTO file_manifest (relative_path, mtime_ns, size, chunk_count, doc_ids)
		VALUES (?, ?, ?, ?, ?)
	`, "bad.go", 1, 10, 1, `{not-json`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("bad.go"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestListInvalidDocIDsJSON(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.db.Exec(`
		INSERT INTO file_manifest (relative_path, mtime_ns, size, chunk_count, doc_ids)
		VALUES (?, ?, ?, ?, ?)
	`, "bad.go", 1, 10, 1, `{not-json`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestJournalModeInvalidPath(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := JournalMode(filepath.Join(blocked, "manifest.db")); err == nil {
		t.Fatal("expected open error")
	}
}

func TestWalSkipReasonSyncedCloudPath(t *testing.T) {
	t.Setenv("MANIFEST_WAL", "")
	dir := filepath.Join(t.TempDir(), "YandexDisk", "project")
	if reason := walSkipReason(filepath.Join(dir, "manifest.db")); reason != "synced_cloud_drive_path" {
		t.Fatalf("reason=%q", reason)
	}
}
