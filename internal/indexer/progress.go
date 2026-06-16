package indexer

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const progressFileName = "progress.json"

// MaxFailedFilesInStatus caps failed_files entries in index_status / progress.json.
const MaxFailedFilesInStatus = 20

// State is indexing lifecycle state.
type State string

const (
	StateIdle    State = "idle"
	StateRunning State = "running"
	StateError   State = "error"
)

// Progress tracks background indexing status persisted to progress.json.
type Progress struct {
	State         State    `json:"state"`
	Running       bool     `json:"running"`
	Force         bool     `json:"force,omitempty"`
	FilesTotal    int      `json:"files_total,omitempty"`
	FilesDone     int      `json:"files_done,omitempty"`
	FilesFailed   int      `json:"files_failed,omitempty"`
	FailedFiles   []string `json:"failed_files,omitempty"`
	ChunksIndexed int      `json:"chunks_indexed,omitempty"`
	ScanMethod    string   `json:"scan_method,omitempty"`
	ScanWarnings  []string `json:"scan_warnings,omitempty"`
	SkippedPaths  []string `json:"skipped_paths,omitempty"`
	CurrentFile   string   `json:"current_file,omitempty"`
	Message       string   `json:"message,omitempty"`
	Error         string   `json:"error,omitempty"`
	StartedAt     string   `json:"started_at,omitempty"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
	FinishedAt    string   `json:"finished_at,omitempty"`
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
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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

// AppendFailedFile records a skipped path in progress (deduplicated, capped).
func AppendFailedFile(p *Progress, rel string) {
	for _, f := range p.FailedFiles {
		if f == rel {
			return
		}
	}
	if len(p.FailedFiles) >= MaxFailedFilesInStatus {
		return
	}
	p.FailedFiles = append(p.FailedFiles, rel)
}

// FinishIdle marks progress complete.
func FinishIdle(p Progress, files, chunks int) Progress {
	now := time.Now().UTC().Format(time.RFC3339)
	p.State = StateIdle
	p.Running = false
	p.FilesTotal = files
	p.FilesDone = files
	p.FilesFailed = 0
	p.FailedFiles = nil
	p.ChunksIndexed = chunks
	p.CurrentFile = ""
	p.Message = "indexing complete"
	p.Error = ""
	p.UpdatedAt = now
	p.FinishedAt = now
	return p
}

// FinishIdleWithWarnings marks progress complete after partial per-file failures.
func FinishIdleWithWarnings(p Progress, filesFailed int) Progress {
	now := time.Now().UTC().Format(time.RFC3339)
	p.State = StateIdle
	p.Running = false
	p.FilesFailed = filesFailed
	p.CurrentFile = ""
	p.Error = ""
	if filesFailed > 0 {
		p.Message = fmt.Sprintf("indexing complete with %d file errors (see diagnostics.log_file; paths in indexing.failed_files)", filesFailed)
	} else {
		p.Message = "indexing complete"
	}
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
		m["percent"] = p.Percent()
	}
	if p.FilesDone > 0 {
		m["files_done"] = p.FilesDone
	}
	if p.FilesFailed > 0 {
		m["files_failed"] = p.FilesFailed
	}
	if len(p.FailedFiles) > 0 {
		m["failed_files"] = append([]string(nil), p.FailedFiles...)
	}
	if p.ChunksIndexed > 0 {
		m["chunks_indexed"] = p.ChunksIndexed
	}
	if p.ScanMethod != "" {
		m["scan_method"] = p.ScanMethod
	}
	if len(p.ScanWarnings) > 0 {
		m["scan_warnings"] = append([]string(nil), p.ScanWarnings...)
	}
	if len(p.SkippedPaths) > 0 {
		m["skipped_paths"] = append([]string(nil), p.SkippedPaths...)
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
	if remaining, ok := p.RemainingSeconds(); ok {
		m["remaining_seconds"] = remaining
	}
	return m
}

// Percent returns indexing completion as a 0..100 value rounded to one decimal.
func (p Progress) Percent() float64 {
	if p.FilesTotal <= 0 {
		return 0
	}
	done := p.FilesDone
	if done < 0 {
		done = 0
	}
	if done > p.FilesTotal {
		done = p.FilesTotal
	}
	percent := (float64(done) / float64(p.FilesTotal)) * 100
	return math.Round(percent*10) / 10
}

// RemainingSeconds estimates remaining indexing time from file throughput.
func (p Progress) RemainingSeconds() (int, bool) {
	if !p.Running || p.FilesTotal <= 0 || p.FilesDone <= 0 || p.FilesDone >= p.FilesTotal || p.StartedAt == "" {
		return 0, false
	}
	startedAt, err := time.Parse(time.RFC3339, p.StartedAt)
	if err != nil {
		return 0, false
	}
	updatedAt := time.Now().UTC()
	if p.UpdatedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, p.UpdatedAt); err == nil {
			updatedAt = parsed
		}
	}
	elapsed := updatedAt.Sub(startedAt)
	if elapsed <= 0 {
		return 0, false
	}
	secondsPerFile := elapsed.Seconds() / float64(p.FilesDone)
	remaining := int(math.Ceil(secondsPerFile * float64(p.FilesTotal-p.FilesDone)))
	if remaining < 0 {
		return 0, false
	}
	return remaining, true
}
