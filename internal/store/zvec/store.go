package zvec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	UpsertChunks(chunks []Chunk, vectors [][]float32) error
	DeleteByIDs(ids []string) error
	Search(vector []float32, topK int, pathGlob string) ([]SearchHit, error)
	WipeCollection() error
}

// Config identifies a workspace zvec collection.
type Config struct {
	IndexDir      string
	WorkspaceRoot string
	ProfileName   string
	Dimensions    int
}

// CollectionName derives stable zvec collection directory name.
func CollectionName(workspaceRoot, profileName string, dimensions int) string {
	raw := fmt.Sprintf("%s:%s:%d", filepath.Clean(workspaceRoot), profileName, dimensions)
	sum := sha256.Sum256([]byte(raw))
	return "ws_" + hex.EncodeToString(sum[:])[:16]
}

// CollectionPath returns the on-disk path for a workspace collection.
func CollectionPath(cfg Config) string {
	return filepath.Join(cfg.IndexDir, "zvec", CollectionName(cfg.WorkspaceRoot, cfg.ProfileName, cfg.Dimensions))
}

// IndexMetaPath returns path to index_meta.json under index dir.
func IndexMetaPath(indexDir string) string {
	return filepath.Join(indexDir, "index_meta.json")
}

// IndexMetaPresent reports whether index_meta.json exists.
func IndexMetaPresent(indexDir string) bool {
	_, err := os.Stat(IndexMetaPath(indexDir))
	return err == nil
}

// ReclaimCollectionLock removes orphaned zvec collection LOCK files under cfg.
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

func reclaimCollectionLockFile(lockPath string) bool {
	return lock.ReclaimOrphanedFile(lockPath)
}
