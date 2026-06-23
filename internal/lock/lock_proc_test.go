package lock

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/testutil"
)

const lockHelperEnv = "MCP_LOCK_TEST_HELPER"

func crossProcessPollDeadline() time.Duration {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test.race") {
			return 10 * time.Second
		}
	}
	return 5 * time.Second
}

func waitForHelperProcess(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	deadline := time.Now().Add(crossProcessPollDeadline())
	for time.Now().Before(deadline) {
		if cmd.Process != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("helper process did not start")
}

func TestCrossProcessExclusiveHold(t *testing.T) {
	dir := t.TempDir()
	child := startLockHelper(t, dir, "hold", 5*time.Second)
	defer func() { _ = child.Process.Kill() }()

	deadline := time.Now().Add(crossProcessPollDeadline())
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

func TestCrossProcessStdioLockFile(t *testing.T) {
	dir := t.TempDir()
	child := startLockHelperFile(t, dir, StdioLockFileName, "hold", 2*time.Second)
	defer func() { _ = child.Process.Kill() }()

	deadline := time.Now().Add(crossProcessPollDeadline())
	for {
		if NewStdio(dir, 300).IsLocked() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("stdio lock file did not appear")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestCrossProcessStdioLiveHolder(t *testing.T) {
	dir := t.TempDir()
	child := startLockHelperFile(t, dir, StdioLockFileName, "hold", 2*time.Second)
	defer func() { _ = child.Process.Kill() }()

	deadline := time.Now().Add(crossProcessPollDeadline())
	for {
		l := NewStdio(dir, 300)
		if !l.IsLocked() {
			if time.Now().After(deadline) {
				t.Fatal("stdio lock file did not appear")
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if pid, ok := l.LiveHolder(); ok && pid != os.Getpid() {
			return
		}
		if time.Now().After(deadline) {
			pid, holderOK := l.HolderPID()
			livePID, liveOK := l.LiveHolder()
			data, _ := os.ReadFile(l.Path())
			t.Fatalf("live holder not detected: holder=%v pid=%d live=%v livePID=%d file=%q alive=%v",
				holderOK, pid, liveOK, livePID, strings.TrimSpace(string(data)), ProcessAlive(pid))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func startLockHelper(t *testing.T, dir, mode string, hold time.Duration) *exec.Cmd {
	return startLockHelperFile(t, dir, lockFileName, mode, hold)
}

func startLockHelperFile(t *testing.T, dir, fileName, mode string, hold time.Duration) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestLockHelperProcess")
	cmd.Env = testutil.HelperProcessEnv(
		lockHelperEnv+"=1",
		"MCP_LOCK_TEST_DIR="+dir,
		"MCP_LOCK_TEST_FILE="+fileName,
		"MCP_LOCK_TEST_MODE="+mode,
		fmt.Sprintf("MCP_LOCK_TEST_HOLD=%d", hold),
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	waitForHelperProcess(t, cmd)
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
	fileName := os.Getenv("MCP_LOCK_TEST_FILE")
	if fileName == "" {
		fileName = lockFileName
	}
	l := NewWithName(dir, fileName, 300)
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

	deadline := time.Now().Add(crossProcessPollDeadline())
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
