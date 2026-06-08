package indexer

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

func TestRecoverStalledProgressIdle(t *testing.T) {
	dir := t.TempDir()
	if err := RecoverStalledProgress(dir, 60); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverStalledProgressRunningNoLock(t *testing.T) {
	dir := t.TempDir()
	store := NewProgressStore(dir)
	if err := store.Save(StartRunning(false)); err != nil {
		t.Fatal(err)
	}
	if err := RecoverStalledProgress(dir, 60); err != nil {
		t.Fatal(err)
	}
	p, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if p.Running {
		t.Fatal("expected recovered idle state")
	}
}

func TestStallWatcher(t *testing.T) {
	w := NewStallWatcher(0.01)
	time.Sleep(20 * time.Millisecond)
	if err := w.Check(); err == nil {
		t.Fatal("expected stall error")
	}
	w.Touch()
	if err := w.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverStalledProgressWithActiveLock(t *testing.T) {
	dir := t.TempDir()
	store := NewProgressStore(dir)
	if err := store.Save(StartRunning(false)); err != nil {
		t.Fatal(err)
	}
	l := lock.New(dir, 300)
	if err := l.TryAcquire(); err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if err := RecoverStalledProgress(dir, 60); err != nil {
		t.Fatal(err)
	}
	p, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !p.Running {
		t.Fatal("expected running progress while lock held")
	}
	_ = filepath.Join(dir, "index.lock")
}
