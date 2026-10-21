package zvec

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

// IndexIdentity describes the workspace owner recorded in index_meta.
type IndexIdentity struct {
	WorkspaceID      string
	WorkspaceRoot    string
	CollectionID     string
	Profile          string
	Dimensions       int
	ChunkingVersion  int
	ChunkingStrategy string
	// ChunkingFingerprint covers the chunking geometry that ChunkingVersion and
	// ChunkingStrategy do not: min_chunk_tokens, prose_overlap_ratio, context_prefix
	// and the per-language toggles. A project override can change them without a
	// version bump, and mixing geometries in one index silently degrades results, so
	// the fingerprint makes such a change visible as an identity mismatch.
	// Empty means "not tracked" (an index written before this field existed): no
	// mismatch is reported for it, so an upgrade does not force a rebuild.
	ChunkingFingerprint string
}

// collectionName derives the zvec collection name for this identity. With a
// collection id the name is path-independent and survives project moves.
func (identity IndexIdentity) collectionName() string {
	if identity.CollectionID != "" {
		return CollectionNameForID(identity.CollectionID, identity.Profile, identity.Dimensions)
	}
	return CollectionName(identity.WorkspaceRoot, identity.Profile, identity.Dimensions)
}

// IndexIdentityMismatch reports whether on-disk index_meta does not match the current identity.
func IndexIdentityMismatch(indexDir string, identity IndexIdentity) (bool, *IndexMeta, error) {
	if !IndexMetaPresent(indexDir) {
		return false, nil, nil
	}
	meta, err := ReadIndexMeta(indexDir)
	if err != nil {
		return false, nil, err
	}
	if err := ValidateIndexMeta(
		indexDir,
		identity.WorkspaceID,
		identity.CollectionID,
		identity.Profile,
		identity.Dimensions,
		identity.ChunkingVersion,
		identity.ChunkingStrategy,
	); err != nil {
		return true, meta, err
	}
	// A changed chunking geometry with the same version/strategy is also an
	// identity change: the index would otherwise mix two geometries.
	if meta.ChunkingFingerprint != "" && identity.ChunkingFingerprint != "" &&
		meta.ChunkingFingerprint != identity.ChunkingFingerprint {
		return true, meta, fmt.Errorf("%w: chunking geometry changed (fingerprint %q vs %q) — run reindex with force=true",
			ErrOwnerMismatch, meta.ChunkingFingerprint, identity.ChunkingFingerprint)
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

// ResetIndexForIdentityChange prepares for a force rebuild after identity mismatch.
// It writes fresh index_meta and removes an orphaned previous collection directory when the
// collection name changed (workspace root / profile / dimensions move). It does not wipe the
// still-serving same-name collection or clear the active manifest — force reindex rebuilds via
// staging and promotes on success.
func ResetIndexForIdentityChange(indexDir string, oldMeta *IndexMeta, store Store, newIdentity IndexIdentity) error {
	if oldMeta == nil {
		oldMeta = &IndexMeta{}
	}
	newName := newIdentity.collectionName()
	if oldMeta.CollectionName != "" && oldMeta.CollectionName != newName {
		if err := RemoveCollectionDir(indexDir, oldMeta.CollectionName); err != nil {
			return err
		}
	}
	return WriteIndexMeta(indexDir, indexMetaFromIdentity(newIdentity, version.ZvecGoVersion))
}

// ReconcileIndex validates or resets index ownership before indexing.
func ReconcileIndex(indexDir string, identity IndexIdentity, force bool, store Store) error {
	if !IndexMetaPresent(indexDir) {
		return EnsureIndexMeta(indexDir, identity)
	}

	mismatch, meta, err := IndexIdentityMismatch(indexDir, identity)
	if err != nil && !mismatch {
		return err
	}
	// Detect a workspace_root move even when WORKSPACE_ID is pinned: the zvec
	// collection name is derived from the root, so a changed name means the
	// existing vectors belong to a different on-disk location. With a
	// collection id the name is path-independent, so a move is legal and only
	// profile/dimensions changes alter the expected name.
	if !mismatch && meta != nil && meta.CollectionName != "" {
		want := identity.collectionName()
		if meta.CollectionName != want {
			mismatch = true
			if identity.CollectionID != "" {
				err = fmt.Errorf("%w: collection %q does not match current collection identity (expected %q)", ErrOwnerMismatch, meta.CollectionName, want)
			} else {
				err = fmt.Errorf("%w: collection %q does not match current workspace root (expected %q)", ErrOwnerMismatch, meta.CollectionName, want)
			}
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
		return EnsureIndexMeta(indexDir, identity)
	}
	return nil
}
