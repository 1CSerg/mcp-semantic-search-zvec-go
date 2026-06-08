package lock

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTryAcquireMkdirBlocked(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New(filepath.Join(blocked, "index"), 300)
	if err := l.TryAcquire(); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestNewDefaultStaleSeconds(t *testing.T) {
	l := New(t.TempDir(), 0)
	if l.staleSecs != 300 {
		t.Fatalf("staleSecs=%v", l.staleSecs)
	}
}

func TestIsStaleAliveProcess(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 300)
	if err := l.TryAcquire(); err != nil {
		t.Fatal(err)
	}
	_ = l.Release()

	l2 := New(dir, 300)
	if l2.isStale() {
		t.Fatal("expected fresh lock from alive process")
	}
	if l2.ReclaimStale() {
		t.Fatal("expected no reclaim for fresh lock")
	}
}

func TestHeartbeatWithoutLock(t *testing.T) {
	l := New(t.TempDir(), 300)
	if err := l.Heartbeat(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLockAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 300)
	if err := l.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if !l.IsHeld() {
		t.Fatal("expected held")
	}
	if err := l.Heartbeat(); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, lockFileName)); !os.IsNotExist(err) {
		t.Fatalf("lock file should be removed: %v", err)
	}
}

func TestLockExclusive(t *testing.T) {
	dir := t.TempDir()
	l1 := New(dir, 300)
	if err := l1.TryAcquire(); err != nil {
		t.Fatal(err)
	}
	l2 := New(dir, 300)
	if err := l2.TryAcquire(); err == nil {
		t.Fatal("expected exclusive lock error")
	}
	_ = l1.Release()
}

func TestReclaimStaleEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, lockFileName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 1)
	if !l.ReclaimStale() {
		t.Fatal("expected stale reclaim")
	}
	if err := l.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire after reclaim: %v", err)
	}
	_ = l.Release()
}

func TestLockPathAndIsLocked(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 300)
	want := filepath.Join(dir, lockFileName)
	if l.Path() != want {
		t.Fatalf("path=%q want=%q", l.Path(), want)
	}
	if l.IsLocked() {
		t.Fatal("expected unlocked")
	}
	if err := l.TryAcquire(); err != nil {
		t.Fatal(err)
	}
	if !l.IsLocked() {
		t.Fatal("expected locked")
	}
	_ = l.Release()
}

func TestReleaseDoesNotRemoveReclaimedLock(t *testing.T) {
	dir := t.TempDir()
	l1 := New(dir, 300)
	if err := l1.TryAcquire(); err != nil {
		t.Fatal(err)
	}
	owner := l1.ownerContent
	if err := l1.file.Close(); err != nil {
		t.Fatal(err)
	}
	l1.file = nil
	l1.ownerContent = owner

	path := filepath.Join(dir, lockFileName)
	// Another process now owns the lock with different content.
	if err := os.WriteFile(path, []byte("2 9999999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := l1.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("current lock should remain: %v", err)
	}
}

func TestReleaseAfterStaleReclaim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, lockFileName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("999999 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	old := New(dir, 1)
	old.ownerContent = "999999 1\n"

	l2 := New(dir, 1)
	if err := l2.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire reclaim: %v", err)
	}

	if err := old.Release(); err != nil {
		t.Fatalf("old Release: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("l2 lock should remain after stale holder Release: %v", err)
	}
	_ = l2.Release()
}
