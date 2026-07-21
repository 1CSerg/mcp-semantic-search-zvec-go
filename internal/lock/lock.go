package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	mu           sync.Mutex
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
	l.mu.Lock()
	defer l.mu.Unlock()
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
	l.mu.Lock()
	defer l.mu.Unlock()
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
	l.mu.Lock()
	owner := l.ownerContent
	if l.file != nil {
		_ = unlock(l.file)
		_ = l.file.Close()
		l.file = nil
	}
	l.ownerContent = ""
	l.mu.Unlock()
	if owner == "" {
		return nil
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
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
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file != nil
}

// LockFileExists reports whether a lock file exists on disk (any owner).
func (l *Lock) LockFileExists() bool {
	_, err := os.Stat(l.path)
	return err == nil
}

// IsLocked reports whether a lock file exists (any owner).
// Deprecated: use LockFileExists; name was misleading (stale files also exist).
func (l *Lock) IsLocked() bool {
	return l.LockFileExists()
}

// HolderPID returns the PID recorded in the lock file, if any.
func (l *Lock) HolderPID() (int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
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
	l.mu.Lock()
	defer l.mu.Unlock()
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
			if !processAlive(payload.PID) {
				return 0, false
			}
			age := time.Since(time.Unix(payload.Heartbeat, 0)).Seconds()
			if age > l.staleSecs {
				return 0, false
			}
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

// ReclaimStale clears an orphaned lock file when no process holds the OS lock.
// The file is truncated in place while the advisory lock is held so another
// process cannot create a competing inode between unlock and removal.
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
	if err := f.Truncate(0); err != nil {
		_ = unlock(f)
		_ = f.Close()
		return false
	}
	_ = unlock(f)
	_ = f.Close()
	return true
}
