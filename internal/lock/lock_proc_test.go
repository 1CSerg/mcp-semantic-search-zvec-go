package lock

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const lockHelperEnv = "MCP_LOCK_TEST_HELPER"

func TestCrossProcessExclusiveHold(t *testing.T) {
	dir := t.TempDir()
	child := startLockHelper(t, dir, "hold", 5*time.Second)
	defer func() { _ = child.Process.Kill() }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		l := New(dir, 300)
		if l.IsLocked() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not acquire lock in time")
		}
		time.Sleep(50 * time.Millisecond)
	}

	l2 := New(dir, 300)
	if err := l2.TryAcquire(); err == nil {
		t.Fatal("expected lock held by child process")
	}
	if l2.ReclaimStale() {
		t.Fatal("expected no reclaim while child holds OS lock")
	}
}

func TestCrossProcessReclaimAfterExit(t *testing.T) {
	dir := t.TempDir()
	child := startLockHelper(t, dir, "hold", 0)
	if err := child.Wait(); err != nil {
		t.Fatalf("helper wait: %v", err)
	}

	l := New(dir, 300)
	if err := l.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire after child exit: %v", err)
	}
	_ = l.Release()
}

func TestCrossProcessReclaimStale(t *testing.T) {
	dir := t.TempDir()
	child := startLockHelper(t, dir, "hold", 10*time.Second)
	time.Sleep(200 * time.Millisecond)
	if err := child.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_, _ = child.Process.Wait()

	l := New(dir, 300)
	if !l.ReclaimStale() {
		t.Fatal("expected reclaim after killed helper")
	}
	if err := l.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire after reclaim: %v", err)
	}
	_ = l.Release()
}

func startLockHelper(t *testing.T, dir, mode string, hold time.Duration) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestLockHelperProcess")
	cmd.Env = append(os.Environ(),
		lockHelperEnv+"=1",
		"MCP_LOCK_TEST_DIR="+dir,
		"MCP_LOCK_TEST_MODE="+mode,
		fmt.Sprintf("MCP_LOCK_TEST_HOLD=%d", hold),
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	return cmd
}

func TestLockHelperProcess(t *testing.T) {
	if os.Getenv(lockHelperEnv) != "1" {
		t.Skip("lock helper subprocess only")
	}
	dir := os.Getenv("MCP_LOCK_TEST_DIR")
	if dir == "" {
		t.Fatal("MCP_LOCK_TEST_DIR not set")
	}
	l := New(dir, 300)
	if err := l.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	hold := os.Getenv("MCP_LOCK_TEST_HOLD")
	if hold == "0" || hold == "" {
		return
	}
	var d time.Duration
	if _, err := fmt.Sscan(hold, &d); err != nil {
		t.Fatalf("parse hold: %v", err)
	}
	time.Sleep(d)
}

func TestCrossProcessLockFileFormat(t *testing.T) {
	dir := t.TempDir()
	child := startLockHelper(t, dir, "hold", 2*time.Second)
	defer func() { _ = child.Process.Kill() }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		path := filepath.Join(dir, lockFileName)
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			if _, ok := New(dir, 300).HolderPID(); ok {
				break
			}
			_ = data
		}
		if time.Now().After(deadline) {
			t.Fatal("lock file with PID not written in time")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
