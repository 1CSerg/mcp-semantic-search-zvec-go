package zvec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

type wipeTrackingStore struct {
	wiped   bool
	wipeErr error
}

func (s *wipeTrackingStore) Open() error  { return nil }
func (s *wipeTrackingStore) Close() error { return nil }
func (s *wipeTrackingStore) DocCount() (int, error) {
	return 0, nil
}
func (s *wipeTrackingStore) UpsertChunks([]Chunk, [][]float32) error { return nil }
func (s *wipeTrackingStore) DeleteByIDs([]string) error              { return nil }
func (s *wipeTrackingStore) Search([]float32, int, string) ([]SearchHit, error) {
	return nil, nil
}
func (s *wipeTrackingStore) WipeCollection() error {
	s.wiped = true
	return s.wipeErr
}

func TestNeedsZvecGoMigrationEmptyCurrent(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{ZvecGoVersion: "v0.3.1"}); err != nil {
		t.Fatal(err)
	}
	need, meta, err := NeedsZvecGoMigration(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if need || meta != nil {
		t.Fatalf("need=%v meta=%+v", need, meta)
	}
}

func TestNeedsZvecGoMigrationZvecDirOnly(t *testing.T) {
	dir := t.TempDir()
	zvecDir := filepath.Join(dir, "zvec")
	if err := os.MkdirAll(zvecDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zvecDir, "segment"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	need, meta, err := NeedsZvecGoMigration(dir, version.ZvecGoVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !need || meta == nil {
		t.Fatalf("need=%v meta=%+v", need, meta)
	}
}

func TestNeedsZvecGoMigrationEmptyIndex(t *testing.T) {
	dir := t.TempDir()
	need, meta, err := NeedsZvecGoMigration(dir, version.ZvecGoVersion)
	if err != nil {
		t.Fatal(err)
	}
	if need || meta != nil {
		t.Fatalf("need=%v meta=%+v", need, meta)
	}
}

func TestNeedsZvecGoMigrationMatch(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{ZvecGoVersion: version.ZvecGoVersion}); err != nil {
		t.Fatal(err)
	}
	need, _, err := NeedsZvecGoMigration(dir, version.ZvecGoVersion)
	if err != nil {
		t.Fatal(err)
	}
	if need {
		t.Fatal("expected no migration")
	}
}

func TestNeedsZvecGoMigrationMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{ZvecGoVersion: "v0.3.1"}); err != nil {
		t.Fatal(err)
	}
	need, meta, err := NeedsZvecGoMigration(dir, version.ZvecGoVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !need || meta.ZvecGoVersion != "v0.3.1" {
		t.Fatalf("need=%v meta=%+v", need, meta)
	}
}

func TestNeedsZvecGoMigrationLegacyEmptyVersion(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{WorkspaceID: "ws1"}); err != nil {
		t.Fatal(err)
	}
	need, _, err := NeedsZvecGoMigration(dir, version.ZvecGoVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !need {
		t.Fatal("expected migration for legacy index without zvec_go_version")
	}
}

func TestNeedsZvecGoMigrationManifestOnly(t *testing.T) {
	dir := t.TempDir()
	manStore, err := manifest.Open(filepath.Join(dir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := manStore.Close(); err != nil {
		t.Fatal(err)
	}
	need, meta, err := NeedsZvecGoMigration(dir, version.ZvecGoVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !need || meta == nil {
		t.Fatalf("need=%v meta=%+v", need, meta)
	}
}

func TestResetIndexForZvecMigration(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:   "ws1",
		ZvecGoVersion: "v0.3.1",
	}); err != nil {
		t.Fatal(err)
	}
	manStore, err := manifest.Open(filepath.Join(dir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := manStore.Upsert(manifest.FileEntry{
		RelativePath: "a.go",
		MtimeNs:      1,
		Size:         1,
		ChunkCount:   1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := manStore.Close(); err != nil {
		t.Fatal(err)
	}

	store := &wipeTrackingStore{}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ResetIndexForZvecMigration(dir, meta, store, version.ZvecGoVersion); err != nil {
		t.Fatal(err)
	}
	if !store.wiped {
		t.Fatal("expected wipe")
	}
	updated, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ZvecGoVersion != version.ZvecGoVersion || updated.WorkspaceID != "ws1" {
		t.Fatalf("meta=%+v", updated)
	}
}

func TestResetIndexForZvecMigrationNilStore(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{ZvecGoVersion: "v0.3.1"}); err != nil {
		t.Fatal(err)
	}
	if err := ResetIndexForZvecMigration(dir, nil, nil, version.ZvecGoVersion); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ZvecGoVersion != version.ZvecGoVersion {
		t.Fatalf("meta=%+v", meta)
	}
}

func TestResetIndexForZvecMigrationWipeError(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{ZvecGoVersion: "v0.3.1"}); err != nil {
		t.Fatal(err)
	}
	store := &wipeTrackingStore{wipeErr: errors.New("wipe failed")}
	err := ResetIndexForZvecMigration(dir, &IndexMeta{ZvecGoVersion: "v0.3.1"}, store, version.ZvecGoVersion)
	if err == nil || err.Error() != "wipe failed" {
		t.Fatalf("err=%v", err)
	}
}
