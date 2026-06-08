package indexer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const progressFileName = "progress.json"

// State is indexing lifecycle state.
type State string

const (
	StateIdle    State = "idle"
	StateRunning State = "running"
	StateError   State = "error"
)

// Progress tracks background indexing status persisted to progress.json.
type Progress struct {
	State         State  `json:"state"`
	Running       bool   `json:"running"`
	Force         bool   `json:"force,omitempty"`
	FilesTotal    int    `json:"files_total,omitempty"`
	FilesDone     int    `json:"files_done,omitempty"`
	ChunksIndexed int    `json:"chunks_indexed,omitempty"`
	CurrentFile   string `json:"current_file,omitempty"`
	Message       string `json:"message,omitempty"`
	Error         string `json:"error,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
}

// ProgressStore reads/writes progress.json under index dir.
type ProgressStore struct {
	path string
	mu   sync.Mutex
}

// NewProgressStore creates a store for indexDir/progress.json.
func NewProgressStore(indexDir string) *ProgressStore {
	return &ProgressStore{path: filepath.Join(indexDir, progressFileName)}
}

// Load reads progress from disk or returns idle default.
func (s *ProgressStore) Load() (Progress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return Progress{State: StateIdle, Running: false}, nil
		}
		return Progress{}, err
	}
	var p Progress
	if err := json.Unmarshal(data, &p); err != nil {
		return Progress{}, err
	}
	return p, nil
}

// Save writes progress atomically.
func (s *ProgressStore) Save(p Progress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	if p.UpdatedAt == "" {
		p.UpdatedAt = now
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// StartRunning initializes a running progress snapshot.
func StartRunning(force bool) Progress {
	now := time.Now().UTC().Format(time.RFC3339)
	return Progress{
		State:     StateRunning,
		Running:   true,
		Force:     force,
		Message:   "indexing started",
		StartedAt: now,
		UpdatedAt: now,
	}
}

// FinishIdle marks progress complete.
func FinishIdle(p Progress, files, chunks int) Progress {
	now := time.Now().UTC().Format(time.RFC3339)
	p.State = StateIdle
	p.Running = false
	p.FilesTotal = files
	p.FilesDone = files
	p.ChunksIndexed = chunks
	p.CurrentFile = ""
	p.Message = "indexing complete"
	p.Error = ""
	p.UpdatedAt = now
	p.FinishedAt = now
	return p
}

// FinishError marks progress failed.
func FinishError(p Progress, err error) Progress {
	now := time.Now().UTC().Format(time.RFC3339)
	p.State = StateError
	p.Running = false
	p.Error = err.Error()
	p.Message = fmt.Sprintf("indexing failed: %v", err)
	p.UpdatedAt = now
	p.FinishedAt = now
	return p
}

// ToIndexingMap returns the indexing block for index_status JSON.
func (p Progress) ToIndexingMap() map[string]any {
	m := map[string]any{
		"state":   string(p.State),
		"running": p.Running,
	}
	if p.Force {
		m["force"] = true
	}
	if p.FilesTotal > 0 {
		m["files_total"] = p.FilesTotal
	}
	if p.FilesDone > 0 {
		m["files_done"] = p.FilesDone
	}
	if p.ChunksIndexed > 0 {
		m["chunks_indexed"] = p.ChunksIndexed
	}
	if p.CurrentFile != "" {
		m["current_file"] = p.CurrentFile
	}
	if p.Message != "" {
		m["message"] = p.Message
	}
	if p.Error != "" {
		m["error"] = p.Error
	}
	if p.StartedAt != "" {
		m["started_at"] = p.StartedAt
	}
	if p.UpdatedAt != "" {
		m["updated_at"] = p.UpdatedAt
	}
	if p.FinishedAt != "" {
		m["finished_at"] = p.FinishedAt
	}
	return m
}
