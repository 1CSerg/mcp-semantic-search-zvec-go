package zvec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

// IndexIdentity describes the workspace owner recorded in index_meta.
type IndexIdentity struct {
	WorkspaceID   string
	WorkspaceRoot string
	Profile       string
	Dimensions    int
}

// IndexIdentityMismatch reports whether on-disk index_meta does not match the current identity.
func IndexIdentityMismatch(indexDir, workspaceID, profile string, dimensions int) (bool, *IndexMeta, error) {
	if !IndexMetaPresent(indexDir) {
		return false, nil, nil
	}
	meta, err := ReadIndexMeta(indexDir)
	if err != nil {
		return false, nil, err
	}
	if err := ValidateIndexMeta(indexDir, workspaceID, profile, dimensions); err != nil {
		return true, meta, err
	}
	return false, meta, nil
}

// RemoveCollectionDir deletes index_dir/zvec/{collectionName}.
func RemoveCollectionDir(indexDir, collectionName string) error {
	if collectionName == "" {
		return nil
	}
	return os.RemoveAll(filepath.Join(indexDir, "zvec", collectionName))
}

// ResetIndexForIdentityChange wipes stale collections and manifest, then writes fresh index_meta.
func ResetIndexForIdentityChange(indexDir string, oldMeta *IndexMeta, store Store, newIdentity IndexIdentity) error {
	if oldMeta == nil {
		oldMeta = &IndexMeta{}
	}
	if err := RemoveCollectionDir(indexDir, oldMeta.CollectionName); err != nil {
		return err
	}
	if store != nil {
		if err := store.WipeCollection(); err != nil && !errors.Is(err, ErrNotLinked) {
			return err
		}
	}
	if err := clearManifest(indexDir); err != nil {
		return err
	}
	return WriteIndexMeta(indexDir, indexMetaFromIdentity(newIdentity, version.ZvecGoVersion))
}

// ReconcileIndex validates or resets index ownership before indexing.
func ReconcileIndex(indexDir string, identity IndexIdentity, force bool, store Store) error {
	if !IndexMetaPresent(indexDir) {
		return EnsureIndexMeta(
			indexDir,
			identity.WorkspaceID,
			identity.WorkspaceRoot,
			identity.Profile,
			identity.Dimensions,
		)
	}

	mismatch, meta, err := IndexIdentityMismatch(indexDir, identity.WorkspaceID, identity.Profile, identity.Dimensions)
	if err != nil && !mismatch {
		return err
	}
	// Detect a workspace_root move even when WORKSPACE_ID is pinned: the zvec
	// collection name is derived from the root, so a changed name means the
	// existing vectors belong to a different on-disk location.
	if !mismatch && meta != nil && meta.CollectionName != "" {
		want := CollectionName(identity.WorkspaceRoot, identity.Profile, identity.Dimensions)
		if meta.CollectionName != want {
			mismatch = true
			err = fmt.Errorf("%w: collection %q does not match current workspace root (expected %q)", ErrOwnerMismatch, meta.CollectionName, want)
		}
	}
	if mismatch {
		if !force {
			return err
		}
		return ResetIndexForIdentityChange(indexDir, meta, store, identity)
	}
	// Identity matches: backfill any missing identity fields in an existing
	// (possibly legacy/partial) index_meta so mixing-protection and root-move
	// detection have complete data on subsequent runs.
	if indexMetaIncomplete(meta) {
		return EnsureIndexMeta(
			indexDir,
			identity.WorkspaceID,
			identity.WorkspaceRoot,
			identity.Profile,
			identity.Dimensions,
		)
	}
	return nil
}

func clearManifest(indexDir string) error {
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
