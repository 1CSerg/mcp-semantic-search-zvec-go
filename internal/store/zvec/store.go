package zvec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

// ErrNotLinked is returned when the binary was built without the zvec CGO tag.
var ErrNotLinked = errors.New("zvec store not linked: build with -tags zvec and CGO_ENABLED=1")

// ErrCollectionMissing is returned when the zvec collection directory does not exist.
var ErrCollectionMissing = errors.New("zvec collection not found")

// ErrOwnerMismatch is returned when index_meta does not match the current workspace identity.
var ErrOwnerMismatch = errors.New("index_owner_mismatch")

// Chunk is a searchable document slice stored in zvec.
type Chunk struct {
	DocID         string
	RelativePath  string
	StartLine     int64
	EndLine       int64
	StartByte     int64
	EndByte       int64
	ChunkType     string
	Name          string
	Snippet       string
	SymbolName    string
	SymbolKind    string
	ParentScope   string
	ChunkStrategy string
}

// SearchHit is one query result.
type SearchHit struct {
	DocID         string
	Path          string
	StartLine     int64
	EndLine       int64
	ChunkType     string
	Name          string
	Snippet       string
	SymbolName    string
	SymbolKind    string
	ParentScope   string
	ChunkStrategy string
	Score         float64
}

// Store abstracts zvec collection operations.
type Store interface {
	Open() error
	IsOpen() bool
	Close() error
	DocCount() (int, error)
	DocIDsPresent(ids []string) (bool, error)
	UpsertChunks(chunks []Chunk, vectors [][]float32) error
	DeleteByIDs(ids []string) error
	Search(vector []float32, topK int, pathGlob string) ([]SearchHit, error)
	WipeCollection() error
}

// Config identifies a workspace zvec collection.
type Config struct {
	IndexDir         string
	WorkspaceRoot    string
	CollectionID     string // global-install identity; overrides WorkspaceRoot for naming
	ProfileName      string
	Dimensions       int
	CollectionSuffix string // e.g. "_staging" for force reindex build
}

// CollectionName derives a stable zvec collection directory name from the workspace root.
func CollectionName(workspaceRoot, profileName string, dimensions int) string {
	return collectionNameWithSuffix(workspaceRoot, profileName, dimensions, "")
}

// CollectionNameForID derives a stable zvec collection directory name from a collection id.
// Unlike CollectionName it does not depend on the workspace path, so moving the
// project keeps the index identity.
func CollectionNameForID(collectionID, profileName string, dimensions int) string {
	return collectionNameHash(fmt.Sprintf("%s:%s:%d", collectionID, profileName, dimensions), "")
}

func collectionNameWithSuffix(workspaceRoot, profileName string, dimensions int, suffix string) string {
	workspaceRoot = config.NormalizeAbsolutePath(filepath.Clean(workspaceRoot))
	return collectionNameHash(fmt.Sprintf("%s:%s:%d", workspaceRoot, profileName, dimensions), suffix)
}

func collectionNameHash(raw, suffix string) string {
	sum := sha256.Sum256([]byte(raw))
	return "ws_" + hex.EncodeToString(sum[:])[:16] + suffix
}

// CollectionName returns the active (non-staging) collection name for cfg.
func (cfg Config) CollectionName() string {
	base := cfg
	base.CollectionSuffix = ""
	return base.collectionName()
}

func (cfg Config) collectionName() string {
	if cfg.CollectionID != "" {
		return collectionNameHash(fmt.Sprintf("%s:%s:%d", cfg.CollectionID, cfg.ProfileName, cfg.Dimensions), cfg.CollectionSuffix)
	}
	return collectionNameWithSuffix(cfg.WorkspaceRoot, cfg.ProfileName, cfg.Dimensions, cfg.CollectionSuffix)
}

// CollectionPath returns the on-disk path for a workspace collection.
func CollectionPath(cfg Config) string {
	return filepath.Join(cfg.IndexDir, "zvec", cfg.collectionName())
}

// StagingCollectionSuffix is appended to the active collection name during force reindex.
const StagingCollectionSuffix = "_staging"

// IndexMetaPath returns path to index_meta.json under index dir.
func IndexMetaPath(indexDir string) string {
	return filepath.Join(indexDir, "index_meta.json")
}

// IndexMetaPresent reports whether index_meta.json exists.
func IndexMetaPresent(indexDir string) bool {
	_, err := os.Stat(IndexMetaPath(indexDir))
	return err == nil
}

// ReclaimCollectionLock truncates orphaned zvec collection LOCK files under cfg.
func ReclaimCollectionLock(cfg Config) bool {
	return reclaimCollectionLockDir(CollectionPath(cfg))
}

func reclaimCollectionLockDir(collectionPath string) bool {
	entries, err := os.ReadDir(collectionPath)
	if err != nil {
		return false
	}
	removed := false
	for _, e := range entries {
		name := e.Name()
		if name == "LOCK" || name == "lock" || name == ".lock" {
			lockPath := filepath.Join(collectionPath, name)
			if reclaimCollectionLockFile(lockPath) {
				removed = true
			}
		}
	}
	return removed
}

// ReclaimAllCollectionLocks truncates orphaned LOCK files under indexDir/zvec/*.
func ReclaimAllCollectionLocks(indexDir string) int {
	zvecRoot := filepath.Join(indexDir, "zvec")
	entries, err := os.ReadDir(zvecRoot)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if reclaimCollectionLockDir(filepath.Join(zvecRoot, e.Name())) {
			removed++
		}
	}
	return removed
}

func reclaimCollectionLockFile(lockPath string) bool {
	return lock.ReclaimOrphanedFile(lockPath)
}
