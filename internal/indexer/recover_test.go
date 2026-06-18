package indexer

import (
	"context"
	"errors"
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

func TestRecoverInterruptedProgress(t *testing.T) {
	dir := t.TempDir()
	store := NewProgressStore(dir)
	p := FinishError(StartRunning(false), context.Canceled)
	p.FilesTotal = 100
	p.FilesDone = 25
	p.ChunksIndexed = 500
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := RecoverInterruptedProgress(dir); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateIdle {
		t.Fatalf("state=%q want idle", got.State)
	}
	if got.Error != "" {
		t.Fatalf("error=%q want empty", got.Error)
	}
	if got.Message != InterruptedMessage {
		t.Fatalf("message=%q", got.Message)
	}
	if got.FilesDone != 25 || got.FilesTotal != 100 || got.ChunksIndexed != 500 {
		t.Fatalf("progress stats lost: %+v", got)
	}
}

func TestRecoverInterruptedProgressLegacySubstring(t *testing.T) {
	dir := t.TempDir()
	store := NewProgressStore(dir)
	p := StartRunning(false)
	p.State = StateError
	p.Running = false
	p.Error = "indexing embed: context canceled"
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := RecoverInterruptedProgress(dir); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateIdle {
		t.Fatalf("state=%q want idle", got.State)
	}
}

func TestRecoverInterruptedProgressNoOp(t *testing.T) {
	dir := t.TempDir()
	store := NewProgressStore(dir)
	if err := store.Save(FinishError(StartRunning(false), errors.New("embed down"))); err != nil {
		t.Fatal(err)
	}
	if err := RecoverInterruptedProgress(dir); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateError {
		t.Fatalf("state=%q want error", got.State)
	}
}
