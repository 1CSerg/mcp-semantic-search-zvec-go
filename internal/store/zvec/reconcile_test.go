package zvec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

func TestIndexIdentityMismatchAbsentMeta(t *testing.T) {
	dir := t.TempDir()
	mismatch, meta, err := IndexIdentityMismatch(dir, testIdentity("ws1", "", "test", 3))
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
		ChunkingVersion:     1,
		ChunkingStrategy:    "hybrid",
	}); err != nil {
		t.Fatal(err)
	}
	mismatch, _, err := IndexIdentityMismatch(dir, testIdentity("ws1", "", "test", 3))
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
	mismatch, meta, err := IndexIdentityMismatch(dir, testIdentity("ws-b", "", "test", 3))
	if err == nil || !mismatch {
		t.Fatalf("mismatch=%v err=%v", mismatch, err)
	}
	if meta.WorkspaceID != "ws-a" {
		t.Fatalf("meta=%+v", meta)
	}
	if !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestIndexIdentityMismatchChunkingVersionZeroVsConfigOne(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "ws1",
		EmbeddingProfile:    "test",
		EmbeddingDimensions: 3,
	}); err != nil {
		t.Fatal(err)
	}
	identity := IndexIdentity{
		WorkspaceID:      "ws1",
		Profile:          "test",
		Dimensions:       3,
		ChunkingVersion:  1,
		ChunkingStrategy: "hybrid",
	}
	mismatch, _, err := IndexIdentityMismatch(dir, identity)
	if err == nil || !mismatch {
		t.Fatalf("mismatch=%v err=%v", mismatch, err)
	}
	if !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestIndexIdentityMismatchChunkingStrategy(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "ws1",
		EmbeddingProfile:    "test",
		EmbeddingDimensions: 3,
		ChunkingVersion:     1,
		ChunkingStrategy:    "line_window",
	}); err != nil {
		t.Fatal(err)
	}
	mismatch, _, err := IndexIdentityMismatch(dir, IndexIdentity{
		WorkspaceID:      "ws1",
		Profile:          "test",
		Dimensions:       3,
		ChunkingVersion:  1,
		ChunkingStrategy: "hybrid",
	})
	if err == nil || !mismatch {
		t.Fatalf("mismatch=%v err=%v", mismatch, err)
	}
	if !errors.Is(err, ErrOwnerMismatch) {
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
	if err == nil || !errors.Is(err, ErrOwnerMismatch) {
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
	if store.wiped {
		t.Fatal("must not wipe active collection before staging promote")
	}
	if _, err := os.Stat(oldCollectionDir); !os.IsNotExist(err) {
		t.Fatalf("old collection dir still exists: err=%v", err)
	}

	updated, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if updated.WorkspaceID != "ws-b" || updated.WorkspaceRoot != config.NormalizeAbsolutePath(newRoot) || updated.CollectionName != newCollection {
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
	if files != 1 {
		t.Fatalf("active manifest must be preserved until promote: files=%d", files)
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

	store := &wipeTrackingStore{}
	identity := IndexIdentity{
		WorkspaceID:   "ws-b",
		WorkspaceRoot: newRoot,
		Profile:       "test",
		Dimensions:    3,
	}
	if err := ReconcileIndex(dir, identity, true, store); err != nil {
		t.Fatal(err)
	}
	if store.wiped {
		t.Fatal("must not wipe active store on identity force reconcile")
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

func TestResetIndexForIdentityChangeSameCollectionPreservesActive(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "ws")
	collection := CollectionName(root, "test", 3)
	activeDir := filepath.Join(dir, "zvec", collection)
	if err := os.MkdirAll(activeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(activeDir, "keep-me")
	if err := os.WriteFile(marker, []byte("active"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "ws-a",
		WorkspaceRoot:       root,
		EmbeddingProfile:    "test",
		EmbeddingDimensions: 3,
		CollectionName:      collection,
		ChunkingVersion:     1,
		ChunkingStrategy:    "hybrid",
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
		DocIDs:       []string{"doc_old"},
	}); err != nil {
		t.Fatal(err)
	}
	_ = manStore.Close()

	store := &wipeTrackingStore{}
	oldMeta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	identity := IndexIdentity{
		WorkspaceID:      "ws-a",
		WorkspaceRoot:    root,
		Profile:          "test",
		Dimensions:       3,
		ChunkingVersion:  2,
		ChunkingStrategy: "hybrid",
	}
	if err := ResetIndexForIdentityChange(dir, oldMeta, store, identity); err != nil {
		t.Fatal(err)
	}
	if store.wiped {
		t.Fatal("same-name identity reset must not wipe active collection")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("active collection marker missing: %v", err)
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
	if files != 1 {
		t.Fatalf("manifest files=%d want 1", files)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ChunkingVersion != 2 {
		t.Fatalf("meta=%+v", meta)
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

func TestReconcileIndexDetectsRootMoveWithPinnedID(t *testing.T) {
	dir := t.TempDir()
	oldRoot := filepath.Join(t.TempDir(), "old")
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "pinned-id",
		WorkspaceRoot:       oldRoot,
		EmbeddingProfile:    "test",
		EmbeddingDimensions: 3,
		CollectionName:      CollectionName(oldRoot, "test", 3),
	}); err != nil {
		t.Fatal(err)
	}
	// Same workspace_id, but the root moved -> collection name differs.
	identity := IndexIdentity{
		WorkspaceID:   "pinned-id",
		WorkspaceRoot: filepath.Join(t.TempDir(), "new"),
		Profile:       "test",
		Dimensions:    3,
	}
	err := ReconcileIndex(dir, identity, false, nil)
	if err == nil || !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("expected collection mismatch, err=%v", err)
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
		WorkspaceID:      "ws1",
		WorkspaceRoot:    root,
		Profile:          "test",
		Dimensions:       3,
		ChunkingVersion:  1,
		ChunkingStrategy: "hybrid",
	}
	if err := ResetIndexForIdentityChange(dir, nil, nil, identity); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.WorkspaceID != "ws1" || meta.ChunkingVersion != 1 || meta.ChunkingStrategy != "hybrid" {
		t.Fatalf("meta=%+v", meta)
	}
}

func TestIndexIdentityMismatchCorruptMeta(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index_meta.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := IndexIdentityMismatch(dir, testIdentity("ws1", "", "test", 3))
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

func TestReconcileIndexBackfillsIncompleteMeta(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "ws")
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:      "ws1",
		ChunkingVersion:  1,
		ChunkingStrategy: "hybrid",
		ZvecGoVersion:    version.ZvecGoVersion,
	}); err != nil {
		t.Fatal(err)
	}
	identity := IndexIdentity{
		WorkspaceID:      "ws1",
		WorkspaceRoot:    root,
		Profile:          "test",
		Dimensions:       3,
		ChunkingVersion:  1,
		ChunkingStrategy: "hybrid",
	}
	if err := ReconcileIndex(dir, identity, false, nil); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.WorkspaceRoot != config.NormalizeAbsolutePath(root) ||
		meta.EmbeddingProfile != "test" ||
		meta.ChunkingVersion != 1 ||
		meta.ChunkingStrategy != "hybrid" {
		t.Fatalf("meta=%+v", meta)
	}
}

func clearManifestForTest(indexDir string) error {
	manifestPath := filepath.Join(indexDir, "manifest.db")
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	manStore, err := manifest.Open(manifestPath)
	if err != nil {
		return err
	}
	if err := manStore.Clear(); err != nil {
		_ = manStore.Close()
		return err
	}
	return manStore.Close()
}

func TestClearManifestMissingFile(t *testing.T) {
	if err := clearManifestForTest(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestClearManifestClearsRows(t *testing.T) {
	dir := t.TempDir()
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
	if err := clearManifestForTest(dir); err != nil {
		t.Fatal(err)
	}
	manStore, err = manifest.Open(filepath.Join(dir, "manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer manStore.Close()
	files, _, err := manStore.Stats()
	if err != nil || files != 0 {
		t.Fatalf("files=%d err=%v", files, err)
	}
}
