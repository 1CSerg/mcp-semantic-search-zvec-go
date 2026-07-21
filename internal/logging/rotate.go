package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// rotatingWriter appends to a file and rotates when maxBytes is exceeded.
type rotatingWriter struct {
	path          string
	maxBytes      int
	backupCount   int
	mu            sync.Mutex
	file          *os.File
	size          int64
	oversizedOnce sync.Once
}

func newRotatingWriter(path string, maxBytes, backupCount int) (*rotatingWriter, error) {
	if maxBytes <= 0 {
		maxBytes = 5242880
	}
	if backupCount <= 0 {
		backupCount = 3
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	w := &rotatingWriter{
		path:        path,
		maxBytes:    maxBytes,
		backupCount: backupCount,
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingWriter) open() error {
	info, err := os.Stat(w.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if info != nil {
		w.size = info.Size()
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w.file = f
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size+int64(len(p)) > int64(w.maxBytes) {
		if int64(len(p)) > int64(w.maxBytes) {
			w.oversizedOnce.Do(func() {
				fmt.Fprintf(os.Stderr, "mcp-semantic-search-zvec-go: log record larger than max_bytes (%d > %d); writing to a fresh rotated segment: %s\n",
					len(p), w.maxBytes, w.path)
			})
		}
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingWriter) rotateLocked() error {
	if w.file != nil {
		if err := w.file.Sync(); err != nil {
			return err
		}
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}
	for i := w.backupCount - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", w.path, i)
		dst := fmt.Sprintf("%s.%d", w.path, i+1)
		if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
			return err
		}
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, dst); err != nil {
				return err
			}
		}
	}
	backup := fmt.Sprintf("%s.1", w.path)
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(w.path); err == nil {
		if err := os.Rename(w.path, backup); err != nil {
			if truncErr := truncateLogFile(w.path); truncErr != nil {
				return fmt.Errorf("rotate rename: %w; truncate fallback: %v", err, truncErr)
			}
		}
	}
	w.size = 0
	return w.open()
}

func truncateLogFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}
