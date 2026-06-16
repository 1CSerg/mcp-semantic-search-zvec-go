package indexer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

func TestRecoverStalledProgressIdle(t *testing.T) {
	dir := t.TempDir()
	if err := RecoverStalledProgress(dir, 60, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverStalledProgressRunningNoLock(t *testing.T) {
	dir := t.TempDir()
	store := NewProgressStore(dir)
	if err := store.Save(StartRunning(false)); err != nil {
		t.Fatal(err)
	}
	if err := RecoverStalledProgress(dir, 60, nil); err != nil {
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

func TestRecoverStalledProgressStaleLockPayload(t *testing.T) {
	dir := t.TempDir()
	store := NewProgressStore(dir)
	if err := store.Save(StartRunning(false)); err != nil {
		t.Fatal(err)
	}
	deadPID := 999999
	if err := os.WriteFile(filepath.Join(dir, "index.lock"), []byte("999999 1 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverStalledProgress(dir, 60, nil); err != nil {
		t.Fatal(err)
	}
	p, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if p.Running {
		t.Fatalf("expected recovered idle state with dead lock pid %d", deadPID)
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
	if err := RecoverStalledProgress(dir, 60, nil); err != nil {
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
