package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// rotatingWriter appends to a file and rotates when maxBytes is exceeded.
type rotatingWriter struct {
	path        string
	maxBytes    int
	backupCount int
	mu          sync.Mutex
	file        *os.File
	size        int64
}

func newRotatingWriter(path string, maxBytes, backupCount int) (*rotatingWriter, error) {
	if maxBytes <= 0 {
		maxBytes = 5242880
	}
	if backupCount <= 0 {
		backupCount = 3
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
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
		_ = w.file.Close()
		w.file = nil
	}
	for i := w.backupCount - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", w.path, i)
		dst := fmt.Sprintf("%s.%d", w.path, i+1)
		_ = os.Remove(dst)
		if _, err := os.Stat(src); err == nil {
			_ = os.Rename(src, dst)
		}
	}
	backup := fmt.Sprintf("%s.1", w.path)
	_ = os.Remove(backup)
	if _, err := os.Stat(w.path); err == nil {
		if err := os.Rename(w.path, backup); err != nil {
			return err
		}
	}
	w.size = 0
	return w.open()
}
