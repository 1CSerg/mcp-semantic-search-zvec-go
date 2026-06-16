package lock

import (
	"fmt"
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
	l1 := New(dir, 300)
	if err := l1.TryAcquire(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l1.Release() }()

	l2 := New(dir, 300)
	if err := l2.TryAcquire(); err == nil {
		t.Fatal("expected exclusive lock error while holder alive")
	}
	if l2.ReclaimStale() {
		t.Fatal("expected no reclaim while OS lock held")
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
	if err := unlock(l1.file); err != nil {
		t.Fatal(err)
	}
	if err := l1.file.Close(); err != nil {
		t.Fatal(err)
	}
	l1.file = nil
	l1.ownerContent = owner

	path := filepath.Join(dir, lockFileName)
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

func TestNewStdioLockName(t *testing.T) {
	dir := t.TempDir()
	l := NewStdio(dir, 300)
	want := filepath.Join(dir, StdioLockFileName)
	if l.Path() != want {
		t.Fatalf("path=%q want=%q", l.Path(), want)
	}
}

func TestLiveHolderDeadPID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StdioLockFileName)
	if err := os.WriteFile(path, []byte("999999 1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewStdio(dir, 300)
	if pid, ok := l.LiveHolder(); ok {
		t.Fatalf("LiveHolder()=%d, want no live holder for dead pid", pid)
	}
	if !l.ReclaimStale() {
		t.Fatal("expected reclaim for orphaned lock with dead pid")
	}
}

func TestTryAcquireReclaimsDeadPIDLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, lockFileName)
	if err := os.WriteFile(path, []byte("999999 1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 300)
	if err := l.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	pid, ok := l.LiveHolder()
	if !ok || pid != os.Getpid() {
		t.Fatalf("LiveHolder()=%d ok=%v want pid=%d", pid, ok, os.Getpid())
	}
	_ = l.Release()
}

func TestLiveHolderCurrentProcess(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 300)
	if err := l.TryAcquire(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Release() }()
	pid, ok := l.LiveHolder()
	if !ok || pid != os.Getpid() {
		t.Fatalf("LiveHolder()=%d ok=%v want pid=%d", pid, ok, os.Getpid())
	}
}

func TestHolderPID(t *testing.T) {
	dir := t.TempDir()
	l := NewStdio(dir, 300)
	if _, ok := l.HolderPID(); ok {
		t.Fatal("expected no holder before acquire")
	}
	if err := l.TryAcquire(); err != nil {
		t.Fatal(err)
	}
	pid, ok := l.HolderPID()
	if !ok || pid != os.Getpid() {
		t.Fatalf("HolderPID()=%d ok=%v want pid=%d", pid, ok, os.Getpid())
	}
	_ = l.Release()
}

func TestLegacyLockFileWithoutOSLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, lockFileName)
	if err := os.WriteFile(path, []byte("999999 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 300)
	if err := l.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire legacy: %v", err)
	}
	pid, ok := l.HolderPID()
	if !ok || pid != os.Getpid() {
		t.Fatalf("HolderPID()=%d ok=%v want pid=%d", pid, ok, os.Getpid())
	}
	_ = l.Release()
}

func TestIsStaleLegacyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, lockFileName)
	if err := os.WriteFile(path, []byte("999999 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 1)
	if !l.isStale() {
		t.Fatal("expected stale legacy lock by mtime")
	}
}

func TestLiveHolderFromHeldLock(t *testing.T) {
	l := New(t.TempDir(), 300)
	if err := l.TryAcquire(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Release() }()
	pid, ok := l.LiveHolder()
	if !ok || pid != os.Getpid() {
		t.Fatalf("LiveHolder()=%d ok=%v", pid, ok)
	}
}

func TestLiveHolderIgnoresDeadPID(t *testing.T) {
	dir := t.TempDir()
	dead := os.Getpid() + 100000
	line := formatLockPayload(dead, 1, time.Now().Unix())
	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 300)
	if pid, ok := l.LiveHolder(); ok {
		t.Fatalf("LiveHolder()=%d ok=%v for dead pid", pid, ok)
	}
}

func TestIsStaleDeadModernPayload(t *testing.T) {
	dir := t.TempDir()
	dead := os.Getpid() + 100000
	line := formatLockPayload(dead, 1, time.Now().Unix())
	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 1)
	if !l.isStale() {
		t.Fatal("expected stale lock for dead pid")
	}
}

func TestIsStaleModernOldHeartbeat(t *testing.T) {
	dir := t.TempDir()
	start := processStartUnix(os.Getpid())
	old := time.Now().Add(-2 * time.Hour).Unix()
	line := formatLockPayload(os.Getpid(), start, old)
	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 60)
	if !l.isStale() {
		t.Fatal("expected stale lock for old heartbeat")
	}
}

func TestIsStalePIDReuse(t *testing.T) {
	dir := t.TempDir()
	line := formatLockPayload(os.Getpid(), 1, time.Now().Unix())
	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 300)
	if !l.isStale() {
		t.Fatal("expected stale lock when start time mismatches")
	}
}

func TestHeartbeat(t *testing.T) {
	l := New(t.TempDir(), 300)
	if err := l.TryAcquire(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Release() }()
	if err := l.Heartbeat(); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
}

func TestLiveHolderPlainPIDDead(t *testing.T) {
	dir := t.TempDir()
	dead := os.Getpid() + 100000
	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte(fmt.Sprintf("%d\n", dead)), 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 300)
	if pid, ok := l.LiveHolder(); ok {
		t.Fatalf("LiveHolder()=%d ok=%v", pid, ok)
	}
}

func TestProcessMatchesLockCurrentProcess(t *testing.T) {
	pid := os.Getpid()
	start := processStartUnix(pid)
	if !processMatchesLock(pid, start) {
		t.Fatal("expected match for current process start time")
	}
	if !processMatchesLock(pid, 0) {
		t.Fatal("expected match when start time is unset")
	}
	if processMatchesLock(pid+100000, start) {
		t.Fatal("expected no match for dead pid")
	}
}

func TestIsStaleEmptyLockFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, lockFileName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 300)
	if !l.isStale() {
		t.Fatal("expected empty lock file to be stale")
	}
}

func TestIsStaleMissingLockFile(t *testing.T) {
	l := New(t.TempDir(), 300)
	if l.isStale() {
		t.Fatal("missing lock file should not be stale")
	}
}

func TestIsStaleAliveModernFresh(t *testing.T) {
	dir := t.TempDir()
	start := processStartUnix(os.Getpid())
	line := formatLockPayload(os.Getpid(), start, time.Now().Unix())
	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 300)
	if l.isStale() {
		t.Fatal("expected fresh lock for alive process")
	}
}

func TestHolderPIDFromLockFile(t *testing.T) {
	dir := t.TempDir()
	line := formatLockPayload(os.Getpid(), processStartUnix(os.Getpid()), time.Now().Unix())
	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 300)
	pid, ok := l.HolderPID()
	if !ok || pid != os.Getpid() {
		t.Fatalf("HolderPID()=%d ok=%v", pid, ok)
	}
}

func TestLiveHolderLegacyAlive(t *testing.T) {
	dir := t.TempDir()
	line := fmt.Sprintf("%d %d\n", os.Getpid(), time.Now().Unix())
	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 300)
	pid, ok := l.LiveHolder()
	if !ok || pid != os.Getpid() {
		t.Fatalf("LiveHolder()=%d ok=%v", pid, ok)
	}
}

func TestLiveHolderPlainPIDAlive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(dir, 300)
	pid, ok := l.LiveHolder()
	if !ok || pid != os.Getpid() {
		t.Fatalf("LiveHolder()=%d ok=%v", pid, ok)
	}
}