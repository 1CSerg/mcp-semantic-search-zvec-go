package zvec

import (
	"errors"
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
	collectionName := CollectionName(newIdentity.WorkspaceRoot, newIdentity.Profile, newIdentity.Dimensions)
	return WriteIndexMeta(indexDir, IndexMeta{
		WorkspaceID:          newIdentity.WorkspaceID,
		WorkspaceRoot:        newIdentity.WorkspaceRoot,
		WorkspaceFingerprint: collectionName,
		EmbeddingProfile:     newIdentity.Profile,
		EmbeddingDimensions:  newIdentity.Dimensions,
		CollectionName:       collectionName,
		ZvecGoVersion:        version.ZvecGoVersion,
	})
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
	if mismatch {
		if !force {
			return err
		}
		return ResetIndexForIdentityChange(indexDir, meta, store, identity)
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
