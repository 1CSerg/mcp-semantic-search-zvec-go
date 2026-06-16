package zvec

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
)

// IndexHasData reports whether the index directory contains persisted index artifacts.
func IndexHasData(indexDir string) bool {
	if IndexMetaPresent(indexDir) {
		return true
	}
	if _, err := os.Stat(filepath.Join(indexDir, "manifest.db")); err == nil {
		return true
	}
	entries, err := os.ReadDir(filepath.Join(indexDir, "zvec"))
	return err == nil && len(entries) > 0
}

// NeedsZvecGoMigration reports whether the on-disk index was built with a different zvec-go version.
func NeedsZvecGoMigration(indexDir, current string) (bool, *IndexMeta, error) {
	if current == "" {
		return false, nil, nil
	}
	if !IndexHasData(indexDir) {
		return false, nil, nil
	}

	meta, err := ReadIndexMeta(indexDir)
	if err != nil {
		if os.IsNotExist(err) || IndexHasData(indexDir) {
			return true, &IndexMeta{}, nil
		}
		return false, nil, err
	}
	if meta.ZvecGoVersion == current {
		return false, meta, nil
	}
	return true, meta, nil
}

// ResetIndexForZvecMigration wipes zvec data and manifest, then writes full index_meta for identity.
func ResetIndexForZvecMigration(indexDir string, _ *IndexMeta, store Store, newVersion string, identity IndexIdentity) error {
	if store != nil {
		if err := store.WipeCollection(); err != nil && !errors.Is(err, ErrNotLinked) {
			return err
		}
	}

	manifestPath := filepath.Join(indexDir, "manifest.db")
	if _, err := os.Stat(manifestPath); err == nil {
		manStore, err := manifest.Open(manifestPath)
		if err != nil {
			return err
		}
		if err := manStore.Clear(); err != nil {
			_ = manStore.Close()
			return err
		}
		if err := manStore.Close(); err != nil {
			return err
		}
	}

	return WriteIndexMeta(indexDir, indexMetaFromIdentity(identity, newVersion))
}
