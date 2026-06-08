package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const lockFileName = "index.lock"

// Lock is an exclusive cross-process index lock file.
type Lock struct {
	path         string
	staleSecs    float64
	file         *os.File
	ownerContent string
}

// New creates a lock helper for indexDir.
func New(indexDir string, staleSeconds float64) *Lock {
	if staleSeconds <= 0 {
		staleSeconds = 300
	}
	return &Lock{
		path:      filepath.Join(indexDir, lockFileName),
		staleSecs: staleSeconds,
	}
}

// Path returns the lock file path.
func (l *Lock) Path() string {
	return l.path
}

// TryAcquire creates the lock exclusively or reclaims a stale lock.
func (l *Lock) TryAcquire() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("mkdir index dir: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		return l.writeAndKeep(f)
	}
	if !os.IsExist(err) {
		return fmt.Errorf("open lock: %w", err)
	}
	if !l.isStale() {
		return fmt.Errorf("index lock held by another process")
	}
	_ = os.Remove(l.path)
	f, err = os.OpenFile(l.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("reclaim lock: %w", err)
	}
	return l.writeAndKeep(f)
}

func (l *Lock) writeAndKeep(f *os.File) error {
	pid := os.Getpid()
	line := fmt.Sprintf("%d %d\n", pid, time.Now().Unix())
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		_ = os.Remove(l.path)
		return fmt.Errorf("write lock: %w", err)
	}
	l.file = f
	l.ownerContent = line
	return nil
}

// Heartbeat updates lock mtime for stale detection.
func (l *Lock) Heartbeat() error {
	if l.file == nil {
		return fmt.Errorf("lock not held")
	}
	now := time.Now()
	return os.Chtimes(l.path, now, now)
}

// Release closes and removes the lock file only if this instance still owns it.
func (l *Lock) Release() error {
	owner := l.ownerContent
	if l.file != nil {
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
		return err
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

// ReclaimStale removes a stale lock if present.
func (l *Lock) ReclaimStale() bool {
	if !l.isStale() {
		return false
	}
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
	fields := strings.Fields(string(data))
	if len(fields) >= 1 {
		if pid, err := strconv.Atoi(fields[0]); err == nil && pid > 0 {
			if processAlive(pid) {
				if len(fields) >= 2 {
					if ts, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						age := time.Since(time.Unix(ts, 0)).Seconds()
						if age <= l.staleSecs {
							return false
						}
					}
				}
			}
		}
	}
	age := time.Since(info.ModTime()).Seconds()
	return age > l.staleSecs
}
