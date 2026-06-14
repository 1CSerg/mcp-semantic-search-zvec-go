package zvec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

func TestIndexIdentityMismatchAbsentMeta(t *testing.T) {
	dir := t.TempDir()
	mismatch, meta, err := IndexIdentityMismatch(dir, "ws1", "test", 3)
	if err != nil {
		t.Fatal(err)
	}
	if mismatch || meta != nil {
		t.Fatalf("mismatch=%v meta=%+v", mismatch, meta)
	}
}

func TestIndexIdentityMismatchMatch(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "ws1",
		EmbeddingProfile:    "test",
		EmbeddingDimensions: 3,
	}); err != nil {
		t.Fatal(err)
	}
	mismatch, _, err := IndexIdentityMismatch(dir, "ws1", "test", 3)
	if err != nil {
		t.Fatal(err)
	}
	if mismatch {
		t.Fatal("expected no mismatch")
	}
}

func TestIndexIdentityMismatchWorkspaceID(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "ws-a",
		EmbeddingProfile:    "test",
		EmbeddingDimensions: 3,
	}); err != nil {
		t.Fatal(err)
	}
	mismatch, meta, err := IndexIdentityMismatch(dir, "ws-b", "test", 3)
	if err == nil || !mismatch {
		t.Fatalf("mismatch=%v err=%v", mismatch, err)
	}
	if meta.WorkspaceID != "ws-a" {
		t.Fatalf("meta=%+v", meta)
	}
	if !strings.Contains(err.Error(), "index_owner_mismatch") {
		t.Fatalf("err=%v", err)
	}
}

func TestReconcileIndexMismatchWithoutForce(t *testing.T) {
	dir := t.TempDir()
	oldRoot := filepath.Join(t.TempDir(), "old")
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "ws-a",
		WorkspaceRoot:       oldRoot,
		EmbeddingProfile:    "test",
		EmbeddingDimensions: 3,
		CollectionName:      CollectionName(oldRoot, "test", 3),
	}); err != nil {
		t.Fatal(err)
	}
	newRoot := filepath.Join(t.TempDir(), "new")
	identity := IndexIdentity{
		WorkspaceID:   "ws-b",
		WorkspaceRoot: newRoot,
		Profile:       "test",
		Dimensions:    3,
	}
	err := ReconcileIndex(dir, identity, false, nil)
	if err == nil || !strings.Contains(err.Error(), "index_owner_mismatch") {
		t.Fatalf("err=%v", err)
	}
}

func TestResetIndexForIdentityChange(t *testing.T) {
	dir := t.TempDir()
	oldRoot := filepath.Join(dir, "old-root")
	newRoot := filepath.Join(dir, "new-root")
	oldCollection := CollectionName(oldRoot, "test", 3)
	newCollection := CollectionName(newRoot, "test", 3)

	oldCollectionDir := filepath.Join(dir, "zvec", oldCollection)
	if err := os.MkdirAll(oldCollectionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldCollectionDir, "segment"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "ws-a",
		WorkspaceRoot:       oldRoot,
		EmbeddingProfile:    "test",
		EmbeddingDimensions: 3,
		CollectionName:      oldCollection,
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
	oldMeta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	identity := IndexIdentity{
		WorkspaceID:   "ws-b",
		WorkspaceRoot: newRoot,
		Profile:       "test",
		Dimensions:    3,
	}
	if err := ResetIndexForIdentityChange(dir, oldMeta, store, identity); err != nil {
		t.Fatal(err)
	}
	if !store.wiped {
		t.Fatal("expected current collection wipe")
	}
	if _, err := os.Stat(oldCollectionDir); !os.IsNotExist(err) {
		t.Fatalf("old collection dir still exists: err=%v", err)
	}

	updated, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if updated.WorkspaceID != "ws-b" || updated.WorkspaceRoot != newRoot || updated.CollectionName != newCollection {
		t.Fatalf("meta=%+v", updated)
	}
	if updated.ZvecGoVersion != version.ZvecGoVersion {
		t.Fatalf("zvec_go_version=%q", updated.ZvecGoVersion)
	}

	manStore, err = manifest.Open(filepath.Join(dir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer manStore.Close()
	files, _, err := manStore.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if files != 0 {
		t.Fatalf("files=%d", files)
	}
}

func TestReconcileIndexMismatchWithForce(t *testing.T) {
	dir := t.TempDir()
	oldRoot := filepath.Join(dir, "old-root")
	newRoot := filepath.Join(dir, "new-root")
	oldCollection := CollectionName(oldRoot, "test", 3)
	oldCollectionDir := filepath.Join(dir, "zvec", oldCollection)
	if err := os.MkdirAll(oldCollectionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "ws-a",
		WorkspaceRoot:       oldRoot,
		EmbeddingProfile:    "test",
		EmbeddingDimensions: 3,
		CollectionName:      oldCollection,
	}); err != nil {
		t.Fatal(err)
	}

	identity := IndexIdentity{
		WorkspaceID:   "ws-b",
		WorkspaceRoot: newRoot,
		Profile:       "test",
		Dimensions:    3,
	}
	if err := ReconcileIndex(dir, identity, true, &wipeTrackingStore{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldCollectionDir); !os.IsNotExist(err) {
		t.Fatalf("old collection dir still exists: err=%v", err)
	}
	updated, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if updated.WorkspaceID != "ws-b" {
		t.Fatalf("meta=%+v", updated)
	}
}

func TestReconcileIndexNoMeta(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "ws")
	identity := IndexIdentity{
		WorkspaceID:   "ws1",
		WorkspaceRoot: root,
		Profile:       "test",
		Dimensions:    3,
	}
	if err := ReconcileIndex(dir, identity, false, nil); err != nil {
		t.Fatal(err)
	}
	if !IndexMetaPresent(dir) {
		t.Fatal("expected index meta created")
	}
}

func TestReconcileIndexNoMismatch(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "ws")
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "ws1",
		WorkspaceRoot:       root,
		EmbeddingProfile:    "test",
		EmbeddingDimensions: 3,
		CollectionName:      CollectionName(root, "test", 3),
	}); err != nil {
		t.Fatal(err)
	}
	identity := IndexIdentity{
		WorkspaceID:   "ws1",
		WorkspaceRoot: root,
		Profile:       "test",
		Dimensions:    3,
	}
	if err := ReconcileIndex(dir, identity, false, nil); err != nil {
		t.Fatal(err)
	}
}

func TestResetIndexForIdentityChangeNilOldMeta(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "ws")
	identity := IndexIdentity{
		WorkspaceID:   "ws1",
		WorkspaceRoot: root,
		Profile:       "test",
		Dimensions:    3,
	}
	if err := ResetIndexForIdentityChange(dir, nil, nil, identity); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.WorkspaceID != "ws1" {
		t.Fatalf("meta=%+v", meta)
	}
}

func TestIndexIdentityMismatchCorruptMeta(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index_meta.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := IndexIdentityMismatch(dir, "ws1", "test", 3)
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestRemoveCollectionDirEmptyName(t *testing.T) {
	dir := t.TempDir()
	if err := RemoveCollectionDir(dir, ""); err != nil {
		t.Fatal(err)
	}
}
