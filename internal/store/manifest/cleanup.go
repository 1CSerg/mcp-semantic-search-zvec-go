package manifest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const cleanupJournalName = "cleanup.jsonl"

// CleanupJournal records doc ids that must be removed from zvec after a crash.
type CleanupJournal struct {
	path string
	mu   sync.Mutex
}

// OpenCleanupJournal opens or creates INDEX_DIR/cleanup.jsonl.
func OpenCleanupJournal(indexDir string) (*CleanupJournal, error) {
	if indexDir == "" {
		return nil, fmt.Errorf("index dir required")
	}
	path := filepath.Join(indexDir, cleanupJournalName)
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	return &CleanupJournal{path: path}, nil
}

type cleanupRecord struct {
	DocIDs []string `json:"doc_ids"`
}

// Append records doc ids pending zvec deletion.
func (j *CleanupJournal) Append(ids []string) error {
	if j == nil || len(ids) == 0 {
		return nil
	}
	rec, err := json.Marshal(cleanupRecord{DocIDs: ids})
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(rec, '\n')); err != nil {
		return err
	}
	return nil
}

// Pending returns all doc ids still recorded in the journal.
func (j *CleanupJournal) Pending() ([]string, error) {
	if j == nil {
		return nil, nil
	}
	f, err := os.Open(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec cleanupRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("decode cleanup journal: %w", err)
		}
		out = append(out, rec.DocIDs...)
	}
	return out, sc.Err()
}

// Clear removes the journal after successful cleanup.
func (j *CleanupJournal) Clear() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return os.Remove(j.path)
}
