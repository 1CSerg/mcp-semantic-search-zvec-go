package zvec

import (
	"fmt"
	"os"
	"path/filepath"
)

const stagingManifestName = "manifest.staging.db"

// StagingManifestPath returns the path to the staging manifest database.
func StagingManifestPath(indexDir string) string {
	return filepath.Join(indexDir, stagingManifestName)
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
		if err := os.Rename(activePath, oldPath); err != nil {
			return fmt.Errorf("rename active collection: %w", err)
		}
	}
	if err := os.Rename(stagingPath, activePath); err != nil {
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
		if err := os.Rename(active, old); err != nil {
			return fmt.Errorf("rename active manifest: %w", err)
		}
	}
	if err := os.Rename(staging, active); err != nil {
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
