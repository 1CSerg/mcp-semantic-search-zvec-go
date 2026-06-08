package manifest

import (
	"database/sql"
	"encoding/json"
	"fmt"

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
	s := &Store{db: db}
	if err := s.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS file_manifest (
			relative_path TEXT PRIMARY KEY,
			mtime_ns INTEGER NOT NULL,
			size INTEGER NOT NULL,
			chunk_count INTEGER NOT NULL DEFAULT 0,
			doc_ids TEXT NOT NULL DEFAULT '[]'
		)
	`)
	return err
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
}

// Get returns manifest entry for path.
func (s *Store) Get(relativePath string) (*FileEntry, error) {
	row := s.db.QueryRow(`
		SELECT relative_path, mtime_ns, size, chunk_count, doc_ids
		FROM file_manifest WHERE relative_path = ?
	`, relativePath)
	var e FileEntry
	var docIDsJSON string
	if err := row.Scan(&e.RelativePath, &e.MtimeNs, &e.Size, &e.ChunkCount, &docIDsJSON); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(docIDsJSON), &e.DocIDs)
	return &e, nil
}

// Upsert inserts or replaces a manifest row.
func (s *Store) Upsert(e FileEntry) error {
	docIDs, err := json.Marshal(e.DocIDs)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO file_manifest (relative_path, mtime_ns, size, chunk_count, doc_ids)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(relative_path) DO UPDATE SET
			mtime_ns = excluded.mtime_ns,
			size = excluded.size,
			chunk_count = excluded.chunk_count,
			doc_ids = excluded.doc_ids
	`, e.RelativePath, e.MtimeNs, e.Size, e.ChunkCount, string(docIDs))
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
		SELECT relative_path, mtime_ns, size, chunk_count, doc_ids
		FROM file_manifest ORDER BY relative_path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileEntry
	for rows.Next() {
		var e FileEntry
		var docIDsJSON string
		if err := rows.Scan(&e.RelativePath, &e.MtimeNs, &e.Size, &e.ChunkCount, &docIDsJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(docIDsJSON), &e.DocIDs)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Clear removes all manifest rows.
func (s *Store) Clear() error {
	_, err := s.db.Exec(`DELETE FROM file_manifest`)
	return err
}
