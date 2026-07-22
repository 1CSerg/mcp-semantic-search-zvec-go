package manifest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"

	_ "modernc.org/sqlite"
)

// Store tracks per-file indexing metadata in SQLite.
type Store struct {
	db *sql.DB
}

// Open opens or creates manifest.db.
func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open manifest db: %w", err)
	}
	// Serialize access on a single connection and wait on cross-process locks
	// instead of failing fast with "database is locked" (status reads and the
	// indexer can hold separate handles to the same file concurrently).
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure manifest db: %w", err)
	}
	if shouldEnableWAL(dbPath) {
		if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("enable manifest WAL: %w", err)
		}
	} else {
		slog.Debug("manifest WAL skipped", "path", dbPath, "reason", walSkipReason(dbPath))
	}
	s := &Store{db: db}
	if err := s.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func shouldEnableWAL(dbPath string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MANIFEST_WAL"))) {
	case "on", "1", "true", "yes":
		return true
	case "off", "0", "false", "no":
		return false
	default:
		return !config.PathIsSyncedCloudDrive(filepath.Dir(dbPath))
	}
}

func walSkipReason(dbPath string) string {
	mode := strings.TrimSpace(os.Getenv("MANIFEST_WAL"))
	if mode != "" {
		return "MANIFEST_WAL=" + mode
	}
	if config.PathIsSyncedCloudDrive(filepath.Dir(dbPath)) {
		return "synced_cloud_drive_path"
	}
	return "auto"
}

// JournalMode returns the current SQLite journal_mode (for tests/diagnostics).
func JournalMode(dbPath string) (string, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		return "", err
	}
	return mode, nil
}

func (s *Store) ensureSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS file_manifest (
			relative_path TEXT PRIMARY KEY,
			mtime_ns INTEGER NOT NULL,
			size INTEGER NOT NULL,
			chunk_count INTEGER NOT NULL DEFAULT 0,
			doc_ids TEXT NOT NULL DEFAULT '[]',
			content_hash TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(`ALTER TABLE file_manifest ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''`)
	return nil
}

// Stats returns aggregate manifest counts.
func (s *Store) Stats() (files int, chunks int, err error) {
	row := s.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(chunk_count), 0)
		FROM file_manifest
	`)
	if err := row.Scan(&files, &chunks); err != nil {
		return 0, 0, err
	}
	return files, chunks, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// FileEntry is one manifest row.
type FileEntry struct {
	RelativePath string
	MtimeNs      int64
	Size         int64
	ChunkCount   int
	DocIDs       []string
	ContentHash  string
}

// Get returns manifest entry for path.
func (s *Store) Get(relativePath string) (*FileEntry, error) {
	row := s.db.QueryRow(`
		SELECT relative_path, mtime_ns, size, chunk_count, doc_ids, content_hash
		FROM file_manifest WHERE relative_path = ?
	`, relativePath)
	var e FileEntry
	var docIDsJSON string
	if err := row.Scan(&e.RelativePath, &e.MtimeNs, &e.Size, &e.ChunkCount, &docIDsJSON, &e.ContentHash); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(docIDsJSON), &e.DocIDs); err != nil {
		return nil, fmt.Errorf("decode doc_ids for %q: %w", relativePath, err)
	}
	return &e, nil
}

// Upsert inserts or replaces a manifest row.
func (s *Store) Upsert(e FileEntry) error {
	docIDs, err := json.Marshal(e.DocIDs)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO file_manifest (relative_path, mtime_ns, size, chunk_count, doc_ids, content_hash)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(relative_path) DO UPDATE SET
			mtime_ns = excluded.mtime_ns,
			size = excluded.size,
			chunk_count = excluded.chunk_count,
			doc_ids = excluded.doc_ids,
			content_hash = excluded.content_hash
	`, e.RelativePath, e.MtimeNs, e.Size, e.ChunkCount, string(docIDs), e.ContentHash)
	return err
}

// Delete removes a manifest row.
func (s *Store) Delete(relativePath string) error {
	_, err := s.db.Exec(`DELETE FROM file_manifest WHERE relative_path = ?`, relativePath)
	return err
}

// List returns all manifest entries.
func (s *Store) List() ([]FileEntry, error) {
	rows, err := s.db.Query(`
		SELECT relative_path, mtime_ns, size, chunk_count, doc_ids, content_hash
		FROM file_manifest ORDER BY relative_path
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FileEntry
	for rows.Next() {
		var e FileEntry
		var docIDsJSON string
		if err := rows.Scan(&e.RelativePath, &e.MtimeNs, &e.Size, &e.ChunkCount, &docIDsJSON, &e.ContentHash); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(docIDsJSON), &e.DocIDs); err != nil {
			return nil, fmt.Errorf("decode doc_ids for %q: %w", e.RelativePath, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AllDocIDs returns distinct non-empty document IDs recorded in the manifest.
func (s *Store) AllDocIDs() ([]string, error) {
	entries, err := s.List()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var out []string
	for _, e := range entries {
		for _, id := range e.DocIDs {
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out, nil
}

// UniqueDocIDCount returns the number of distinct doc ids recorded in the manifest.
func (s *Store) UniqueDocIDCount() (int, error) {
	entries, err := s.List()
	if err != nil {
		return 0, err
	}
	seen := make(map[string]struct{})
	for _, e := range entries {
		for _, id := range e.DocIDs {
			if id == "" {
				continue
			}
			seen[id] = struct{}{}
		}
	}
	return len(seen), nil
}

// Clear removes all manifest rows.
func (s *Store) Clear() error {
	_, err := s.db.Exec(`DELETE FROM file_manifest`)
	return err
}
