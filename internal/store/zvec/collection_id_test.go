package zvec

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectionNameForIDStableAndDistinct(t *testing.T) {
	a := CollectionNameForID("id-1", "smoke", 128)
	b := CollectionNameForID("id-1", "smoke", 128)
	if a != b {
		t.Fatalf("not stable: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "ws_") || len(a) != len("ws_")+16 {
		t.Fatalf("unexpected format: %q", a)
	}
	if c := CollectionNameForID("id-2", "smoke", 128); c == a {
		t.Fatal("different collection ids must give different names")
	}
	if d := CollectionNameForID("id-1", "other", 128); d == a {
		t.Fatal("different profiles must give different names")
	}
	if legacy := CollectionName("/proj", "smoke", 128); legacy == a {
		t.Fatal("collection-id name must differ from legacy workspace-root name")
	}
}

func TestCollectionPathWithCollectionID(t *testing.T) {
	cfg := Config{
		IndexDir:      "/idx",
		WorkspaceRoot: "/proj",
		CollectionID:  "id-1",
		ProfileName:   "smoke",
		Dimensions:    128,
	}
	want := filepath.Join("/idx", "zvec", CollectionNameForID("id-1", "smoke", 128))
	if got := CollectionPath(cfg); got != want {
		t.Fatalf("CollectionPath=%q want %q", got, want)
	}
	if got := cfg.CollectionName(); got != CollectionNameForID("id-1", "smoke", 128) {
		t.Fatalf("CollectionName=%q", got)
	}

	// Without CollectionID the legacy workspace-root naming applies.
	legacy := Config{IndexDir: "/idx", WorkspaceRoot: "/proj", ProfileName: "smoke", Dimensions: 128}
	if got := CollectionPath(legacy); got != filepath.Join("/idx", "zvec", CollectionName("/proj", "smoke", 128)) {
		t.Fatalf("legacy CollectionPath=%q", got)
	}

	// Staging suffix applies on top of the collection-id name.
	staging := cfg
	staging.CollectionSuffix = StagingCollectionSuffix
	if got := CollectionPath(staging); !strings.HasSuffix(got, StagingCollectionSuffix) {
		t.Fatalf("staging CollectionPath=%q", got)
	}
}

func TestIdentityMoveWithCollectionIDIsNotMismatch(t *testing.T) {
	dir := tempDir(t)
	oldIdentity := testIdentity("ws1", "/old/proj", "smoke", 128)
	oldIdentity.CollectionID = "id-1"
	if err := EnsureIndexMeta(dir, oldIdentity); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.CollectionID != "id-1" {
		t.Fatalf("collection_id=%q", meta.CollectionID)
	}
	if meta.CollectionName != CollectionNameForID("id-1", "smoke", 128) {
		t.Fatalf("collection_name=%q", meta.CollectionName)
	}

	// Project moved to a new root: same collection_id must not mismatch.
	moved := testIdentity("", "/new/location/proj", "smoke", 128)
	moved.CollectionID = "id-1"
	mismatch, _, err := IndexIdentityMismatch(dir, moved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mismatch {
		t.Fatal("workspace move with same collection_id must not be a mismatch")
	}
	if err := ReconcileIndex(dir, moved, false, nil); err != nil {
		t.Fatalf("reconcile after move: %v", err)
	}
}

func TestCollectionIDChangeIsMismatch(t *testing.T) {
	dir := tempDir(t)
	oldIdentity := testIdentity("ws1", "/proj", "smoke", 128)
	oldIdentity.CollectionID = "id-1"
	if err := EnsureIndexMeta(dir, oldIdentity); err != nil {
		t.Fatal(err)
	}

	other := testIdentity("ws1", "/proj", "smoke", 128)
	other.CollectionID = "id-2"
	mismatch, _, err := IndexIdentityMismatch(dir, other)
	if !mismatch {
		t.Fatal("expected mismatch on collection_id change")
	}
	if err == nil || !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("err=%v", err)
	}

	// Force reconcile resets the index to the new identity.
	if err := ReconcileIndex(dir, other, true, nil); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.CollectionID != "id-2" {
		t.Fatalf("collection_id=%q", meta.CollectionID)
	}
}

func TestValidateIndexMetaCollectionIDSkipsWorkspaceID(t *testing.T) {
	dir := tempDir(t)
	oldIdentity := testIdentity("ws-old", "/old/proj", "smoke", 128)
	oldIdentity.CollectionID = "id-1"
	if err := EnsureIndexMeta(dir, oldIdentity); err != nil {
		t.Fatal(err)
	}

	// Different workspace_id but the same collection_id: not an owner mismatch.
	if err := ValidateIndexMeta(dir, "ws-new", "id-1", "smoke", 128, 1, "hybrid"); err != nil {
		t.Fatalf("err=%v", err)
	}
	// Different collection_id: owner mismatch.
	if err := ValidateIndexMeta(dir, "ws-new", "id-2", "smoke", 128, 1, "hybrid"); err == nil || !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestReconcileIndexRootMoveLegacyStillMismatch(t *testing.T) {
	dir := tempDir(t)
	oldRoot := tempDir(t)
	newRoot := tempDir(t)
	identity := testIdentity("ws1", oldRoot, "test", 3)
	if err := EnsureIndexMeta(dir, identity); err != nil {
		t.Fatal(err)
	}
	moved := testIdentity("ws1", newRoot, "test", 3)
	mismatch, _, err := IndexIdentityMismatch(dir, moved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Legacy identity (no collection_id): workspace_id matches, but the root move
	// surfaces via ReconcileIndex collection-name check.
	if mismatch {
		t.Fatal("workspace_id match should not mismatch at validate stage")
	}
	if err := ReconcileIndex(dir, moved, false, nil); err == nil || !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("expected root-move owner mismatch, err=%v", err)
	}
}
