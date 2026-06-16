package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	lockFileName      = "index.lock"
	StdioLockFileName = "stdio.lock"
)

// Lock is an exclusive cross-process index lock file.
type Lock struct {
	path         string
	staleSecs    float64
	file         *os.File
	ownerContent string
}

// New creates an indexing lock helper for indexDir.
func New(indexDir string, staleSeconds float64) *Lock {
	return NewWithName(indexDir, lockFileName, staleSeconds)
}

// NewStdio creates a per-workspace stdio MCP singleton lock.
func NewStdio(indexDir string, staleSeconds float64) *Lock {
	return NewWithName(indexDir, StdioLockFileName, staleSeconds)
}

// NewWithName creates a lock helper with a custom file name under indexDir.
func NewWithName(indexDir, name string, staleSeconds float64) *Lock {
	if staleSeconds <= 0 {
		staleSeconds = 300
	}
	return &Lock{
		path:      filepath.Join(indexDir, name),
		staleSecs: staleSeconds,
	}
}

// Path returns the lock file path.
func (l *Lock) Path() string {
	return l.path
}

// TryAcquire creates the lock exclusively using an OS-level advisory lock.
// Orphaned lock files (dead holder PID, released OS lock) are reclaimed first.
func (l *Lock) TryAcquire() error {
	_ = l.ReclaimStale()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return fmt.Errorf("mkdir index dir: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock: %w", err)
	}
	if err := lockExclusiveNB(f); err != nil {
		_ = f.Close()
		if isLockHeld(err) {
			return fmt.Errorf("lock held by another process: %s", filepath.Base(l.path))
		}
		return fmt.Errorf("lock file: %w", err)
	}
	return l.writeAndKeep(f)
}

func (l *Lock) writeAndKeep(f *os.File) error {
	pid := os.Getpid()
	startTime := processStartUnix(pid)
	line := formatLockPayload(pid, startTime, time.Now().Unix())
	if err := f.Truncate(0); err != nil {
		_ = unlock(f)
		_ = f.Close()
		return fmt.Errorf("truncate lock: %w", err)
	}
	if _, err := f.WriteString(line); err != nil {
		_ = unlock(f)
		_ = f.Close()
		return fmt.Errorf("write lock: %w", err)
	}
	l.file = f
	l.ownerContent = line
	return nil
}

// Heartbeat refreshes the timestamp in the lock payload for diagnostics.
// OS-level locking does not require heartbeat for mutual exclusion.
func (l *Lock) Heartbeat() error {
	if l.file == nil {
		return fmt.Errorf("lock not held")
	}
	pid := os.Getpid()
	startTime := processStartUnix(pid)
	line := formatLockPayload(pid, startTime, time.Now().Unix())
	if err := l.file.Truncate(0); err != nil {
		return err
	}
	if _, err := l.file.Seek(0, 0); err != nil {
		return err
	}
	if _, err := l.file.WriteString(line); err != nil {
		return err
	}
	l.ownerContent = line
	return nil
}

// Release closes and removes the lock file only if this instance still owns it.
func (l *Lock) Release() error {
	owner := l.ownerContent
	if l.file != nil {
		_ = unlock(l.file)
		_ = l.file.Close()
		l.file = nil
	}
	l.ownerContent = ""
	if owner == "" {
		return nil
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		// Another process may hold an OS lock; cannot verify ownership safely.
		return nil
	}
	if string(data) != owner {
		return nil
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsHeld reports whether this process holds the lock.
func (l *Lock) IsHeld() bool {
	return l.file != nil
}

// IsLocked reports whether a lock file exists (any owner).
func (l *Lock) IsLocked() bool {
	_, err := os.Stat(l.path)
	return err == nil
}

// HolderPID returns the PID recorded in the lock file, if any.
func (l *Lock) HolderPID() (int, bool) {
	var data []byte
	if l.file != nil && l.ownerContent != "" {
		data = []byte(l.ownerContent)
	} else {
		var err error
		data, err = os.ReadFile(l.path)
		if err != nil {
			return 0, false
		}
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}
	if payload, ok := parseLockPayload(string(data)); ok {
		return payload.PID, true
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// LiveHolder returns the PID of the current lock holder only when that process
// is alive and matches the identity recorded in the lock file.
func (l *Lock) LiveHolder() (int, bool) {
	if l.file != nil && l.ownerContent != "" {
		return l.liveHolderFromPayload(l.ownerContent)
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		return 0, false
	}
	return l.liveHolderFromPayload(string(data))
}

func (l *Lock) liveHolderFromPayload(data string) (int, bool) {
	if payload, ok := parseLockPayload(data); ok {
		if !processAlive(payload.PID) {
			return 0, false
		}
		if payload.Legacy {
			return payload.PID, true
		}
		if !processMatchesLock(payload.PID, payload.StartTime) {
			return 0, false
		}
		return payload.PID, true
	}
	fields := strings.Fields(strings.TrimSpace(data))
	if len(fields) == 0 {
		return 0, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 || !processAlive(pid) {
		return 0, false
	}
	return pid, true
}

// ReclaimStale removes an orphaned lock file when no process holds the OS lock.
func (l *Lock) ReclaimStale() bool {
	if _, err := os.Stat(l.path); err != nil {
		return false
	}
	f, err := os.OpenFile(l.path, os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	if err := lockExclusiveNB(f); err != nil {
		_ = f.Close()
		return false
	}
	_ = unlock(f)
	_ = f.Close()
	_ = os.Remove(l.path)
	return true
}

func (l *Lock) isStale() bool {
	info, err := os.Stat(l.path)
	if err != nil {
		return false
	}
	if info.Size() == 0 {
		return true
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		return true
	}
	if payload, ok := parseLockPayload(string(data)); ok {
		if !processAlive(payload.PID) {
			return true
		}
		if payload.Legacy {
			age := time.Since(time.Unix(payload.Heartbeat, 0)).Seconds()
			return age > l.staleSecs
		}
		if !processMatchesLock(payload.PID, payload.StartTime) {
			// PID is alive but its start time differs from the one recorded in
			// the lock: the original holder died and the PID was reused by an
			// unrelated process, so the lock is stale.
			return true
		}
		age := time.Since(time.Unix(payload.Heartbeat, 0)).Seconds()
		return age > l.staleSecs
	}
	age := time.Since(info.ModTime()).Seconds()
	return age > l.staleSecs
}
