package zvec

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

// IndexMeta binds an index directory to a workspace owner.
type IndexMeta struct {
	WorkspaceID          string `json:"workspace_id"`
	WorkspaceRoot        string `json:"workspace_root"`
	CollectionID         string `json:"collection_id,omitempty"`
	WorkspaceFingerprint string `json:"workspace_fingerprint"`
	EmbeddingProfile     string `json:"embedding_profile"`
	EmbeddingDimensions  int    `json:"embedding_dimensions"`
	CollectionName       string `json:"collection_name"`
	ChunkingVersion      int    `json:"chunking_version"`
	ChunkingStrategy     string `json:"chunking_strategy"`
	// ChunkingFingerprint detects a geometry change (min tokens, prose overlap,
	// context prefix, language toggles) that does not bump the chunking version.
	ChunkingFingerprint string `json:"chunking_fingerprint,omitempty"`
	ZvecGoVersion       string `json:"zvec_go_version,omitempty"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

// ReadIndexMeta loads index_meta.json.
func ReadIndexMeta(indexDir string) (*IndexMeta, error) {
	path := IndexMetaPath(indexDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta IndexMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// WriteIndexMeta writes index_meta.json.
func WriteIndexMeta(indexDir string, meta IndexMeta) error {
	if err := os.MkdirAll(indexDir, 0o700); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if meta.CreatedAt == "" {
		meta.CreatedAt = now
	}
	meta.UpdatedAt = now
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := IndexMetaPath(indexDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, IndexMetaPath(indexDir)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ValidateIndexMeta checks owner/profile/dimensions/chunking match.
// When both the stored meta and the current identity carry a collection_id,
// ownership is decided by collection_id alone and a workspace_root move is legal.
func ValidateIndexMeta(indexDir, workspaceID, collectionID, profile string, dimensions, chunkingVersion int, chunkingStrategy string) error {
	if !IndexMetaPresent(indexDir) {
		return nil
	}
	meta, err := ReadIndexMeta(indexDir)
	if err != nil {
		return fmt.Errorf("read index_meta: %w", err)
	}
	if meta.CollectionID != "" && collectionID != "" {
		if meta.CollectionID != collectionID {
			return fmt.Errorf("%w: index belongs to collection_id %q, current is %q", ErrOwnerMismatch, meta.CollectionID, collectionID)
		}
	} else if meta.WorkspaceID != "" && workspaceID != "" && !workspaceIDsEqual(meta.WorkspaceID, workspaceID) {
		return fmt.Errorf("%w: index belongs to workspace_id %q, current is %q", ErrOwnerMismatch, meta.WorkspaceID, workspaceID)
	}
	if meta.EmbeddingProfile != "" && profile != "" && meta.EmbeddingProfile != profile {
		return fmt.Errorf("index profile mismatch: %q vs %q", meta.EmbeddingProfile, profile)
	}
	if meta.EmbeddingDimensions > 0 && dimensions > 0 && meta.EmbeddingDimensions != dimensions {
		return fmt.Errorf("index dimensions mismatch: %d vs %d", meta.EmbeddingDimensions, dimensions)
	}
	if chunkingVersion > 0 && meta.ChunkingVersion != chunkingVersion {
		return fmt.Errorf("%w: chunking_version mismatch: %d vs %d", ErrOwnerMismatch, meta.ChunkingVersion, chunkingVersion)
	}
	if chunkingStrategy != "" && meta.ChunkingStrategy != chunkingStrategy {
		return fmt.Errorf("%w: chunking_strategy mismatch: %q vs %q", ErrOwnerMismatch, meta.ChunkingStrategy, chunkingStrategy)
	}
	return nil
}

func workspaceIDsEqual(a, b string) bool {
	if a == b {
		return true
	}
	return config.NormalizeAbsolutePath(a) == config.NormalizeAbsolutePath(b)
}

func indexMetaFromIdentity(identity IndexIdentity, zvecGoVersion string) IndexMeta {
	root := config.NormalizeAbsolutePath(identity.WorkspaceRoot)
	wsID := config.NormalizeWorkspaceID(identity.WorkspaceID, root)
	collectionName := identity.collectionName()
	return IndexMeta{
		WorkspaceID:          wsID,
		WorkspaceRoot:        root,
		CollectionID:         identity.CollectionID,
		WorkspaceFingerprint: collectionName,
		EmbeddingProfile:     identity.Profile,
		EmbeddingDimensions:  identity.Dimensions,
		CollectionName:       collectionName,
		ChunkingVersion:      identity.ChunkingVersion,
		ChunkingStrategy:     identity.ChunkingStrategy,
		ChunkingFingerprint:  identity.ChunkingFingerprint,
		ZvecGoVersion:        zvecGoVersion,
	}
}

func indexMetaIncomplete(meta *IndexMeta) bool {
	if meta == nil {
		return true
	}
	return meta.WorkspaceRoot == "" ||
		meta.CollectionName == "" ||
		meta.EmbeddingProfile == "" ||
		meta.EmbeddingDimensions == 0
}

// WriteIdentityMeta overwrites index_meta.json with a complete meta for identity.
// Tooling that creates a collection outside the normal indexer (cmd/seed-index)
// must call this: without a complete meta the server classifies the collection
// as a legacy zvec-go index and wipes it on startup.
func WriteIdentityMeta(indexDir string, identity IndexIdentity) error {
	return WriteIndexMeta(indexDir, indexMetaFromIdentity(identity, version.ZvecGoVersion))
}

// EnsureIndexMeta creates, validates, or backfills index_meta.json.
func EnsureIndexMeta(indexDir string, identity IndexIdentity) error {
	if !IndexMetaPresent(indexDir) {
		return WriteIndexMeta(indexDir, indexMetaFromIdentity(identity, version.ZvecGoVersion))
	}
	meta, err := ReadIndexMeta(indexDir)
	if err != nil {
		return fmt.Errorf("read index_meta: %w", err)
	}
	if indexMetaIncomplete(meta) {
		zvecGoVersion := meta.ZvecGoVersion
		if zvecGoVersion == "" {
			zvecGoVersion = version.ZvecGoVersion
		}
		return WriteIndexMeta(indexDir, indexMetaFromIdentity(identity, zvecGoVersion))
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
		return err
	}
	return nil
}
