package zvec

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const stagingManifestName = "manifest.staging.db"

// promoteRenameAttempts bounds the retry of a promotion rename. On Windows a
// concurrent reader (a status poll opening manifest.db) makes the rename fail
// with a sharing violation, and that reader closes within milliseconds.
const promoteRenameAttempts = 20

var promoteRenameBackoff = 25 * time.Millisecond

// renameWithRetry renames oldPath to newPath, retrying briefly on Windows where a
// transient open handle blocks the rename. Other platforms rename once: there a
// concurrent reader never fails the call.
func renameWithRetry(oldPath, newPath string) error {
	err := os.Rename(oldPath, newPath)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	for attempt := 1; attempt < promoteRenameAttempts; attempt++ {
		time.Sleep(promoteRenameBackoff)
		if err = os.Rename(oldPath, newPath); err == nil {
			return nil
		}
	}
	return err
}

// StagingManifestPath returns the path to the staging manifest database.
func StagingManifestPath(indexDir string) string {
	return filepath.Join(indexDir, stagingManifestName)
}

// writableOpener is implemented by stores that can open their collection for
// writing and create it when it does not exist yet.
type writableOpener interface {
	OpenWritable() error
}

// OpenStaging opens cfg for a staged force reindex build.
//
// A staged build starts from an empty staging collection, so the store must open
// for writing and create the collection when it is absent. Read-only Open returns
// ErrCollectionMissing on a clean index and would abort every force reindex.
func OpenStaging(cfg Config) (Store, error) {
	return openStaging(cfg, New)
}

func openStaging(cfg Config, newStore func(Config) Store) (Store, error) {
	s := newStore(cfg)
	if w, ok := s.(writableOpener); ok {
		if err := w.OpenWritable(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err := s.Open(); err != nil {
		return nil, err
	}
	return s, nil
}

// PromoteStagingCollection atomically replaces the active collection with the staging build.
func PromoteStagingCollection(cfg Config) error {
	activePath := CollectionPath(cfg)
	stagingCfg := cfg
	stagingCfg.CollectionSuffix = StagingCollectionSuffix
	stagingPath := CollectionPath(stagingCfg)
	if _, err := os.Stat(stagingPath); err != nil {
		return fmt.Errorf("staging collection missing: %w", err)
	}
	oldPath := activePath + ".old"
	_ = os.RemoveAll(oldPath)
	if _, err := os.Stat(activePath); err == nil {
		if err := renameWithRetry(activePath, oldPath); err != nil {
			return fmt.Errorf("rename active collection: %w", err)
		}
	}
	if err := renameWithRetry(stagingPath, activePath); err != nil {
		if _, statErr := os.Stat(oldPath); statErr == nil {
			_ = os.Rename(oldPath, activePath)
		}
		return fmt.Errorf("promote staging collection: %w", err)
	}
	return os.RemoveAll(oldPath)
}

// DiscardStagingCollection removes an incomplete staging build.
func DiscardStagingCollection(cfg Config) error {
	stagingCfg := cfg
	stagingCfg.CollectionSuffix = StagingCollectionSuffix
	return os.RemoveAll(CollectionPath(stagingCfg))
}

// PromoteStagingManifest swaps manifest.staging.db into manifest.db.
func PromoteStagingManifest(indexDir string) error {
	active := filepath.Join(indexDir, "manifest.db")
	staging := StagingManifestPath(indexDir)
	if _, err := os.Stat(staging); err != nil {
		return fmt.Errorf("staging manifest missing: %w", err)
	}
	old := active + ".old"
	_ = os.Remove(old)
	if _, err := os.Stat(active); err == nil {
		if err := renameWithRetry(active, old); err != nil {
			return fmt.Errorf("rename active manifest: %w", err)
		}
	}
	if err := renameWithRetry(staging, active); err != nil {
		if _, statErr := os.Stat(old); statErr == nil {
			_ = os.Rename(old, active)
		}
		return fmt.Errorf("promote staging manifest: %w", err)
	}
	return os.Remove(old)
}

// DiscardStagingManifest removes staging manifest without touching the active index.
func DiscardStagingManifest(indexDir string) error {
	return os.Remove(StagingManifestPath(indexDir))
}
