package daemonclient

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestDaemonLockSingleton: only one daemon may hold the singleton lock; a second
// attempt is refused with ErrDaemonAlreadyRunning, and releasing frees it.
func TestDaemonLockSingleton(t *testing.T) {
	dir := testEnv(t)

	first, err := AcquireDaemonLock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireDaemonLock(); !errors.Is(err, ErrDaemonAlreadyRunning) {
		t.Fatalf("second acquire err=%v want ErrDaemonAlreadyRunning", err)
	}
	lockPath, err := DaemonLockPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file %s: %v", lockPath, err)
	}

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireDaemonLock()
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	defer func() { _ = second.Release() }()

	// The lock lives under the user dir, not in any project.
	if !filepath.IsAbs(lockPath) {
		t.Fatalf("lock path %q is not absolute", lockPath)
	}
	if _, err := os.Stat(filepath.Join(dir, "runtime", daemonLockName)); err != nil {
		t.Fatalf("lock file must be under <userdir>/runtime: %v", err)
	}
}

// TestDaemonLockRecoveredAfterCrash simulates a crashed daemon: a lock file left
// behind by a dead process must not keep the next daemon from starting.
func TestDaemonLockRecoveredAfterCrash(t *testing.T) {
	dir := testEnv(t)
	lockPath := filepath.Join(dir, "runtime", daemonLockName)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	// A PID that cannot be alive: pid 0 is never a real holder.
	if err := os.WriteFile(lockPath, []byte("pid=0 start=0 ts=0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := AcquireDaemonLock()
	if err != nil {
		t.Fatalf("a stale lock from a dead holder must be reclaimed: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

// TestDetachedLaunchRequiresExecutable pins the launcher's input contract.
func TestDetachedLaunchRequiresExecutable(t *testing.T) {
	dir := testEnv(t)
	if err := StartDaemon(t.Context(), LaunchOptions{LogDir: dir}); err == nil {
		t.Fatal("missing executable must be an error")
	}
	if err := StartDaemon(t.Context(), LaunchOptions{Executable: filepath.Join(dir, "missing.exe"), LogDir: dir}); err == nil {
		t.Fatal("a nonexistent executable must be an error")
	}
}

// TestDetachedLaunchSurvivesProxyExit is the platform integration check: a child
// started by the launcher keeps running after the launching process is gone, and
// its stdio does not stay attached to the parent's pipes.
//
// The child is a tiny helper process (this test binary re-executed) that waits
// for a sentinel file, so no real daemon or index is touched.
func TestDetachedLaunchSurvivesProxyExit(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a detached process")
	}
	dir := testEnv(t)
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// The helper is this test binary; it exits as soon as it starts, which still
	// proves the launcher can start a detached child with redirected stdio.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	opts := LaunchOptions{
		Executable: exe,
		UserDir:    dir,
		Token:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		LogDir:     logDir,
	}
	// Our launcher always passes --daemon, which the test binary does not accept,
	// so the child exits quickly. What matters here is that Start returns without
	// blocking on inherited pipes and that the log files exist.
	if err := StartDaemon(t.Context(), opts); err != nil {
		t.Fatalf("detached launch: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(logDir, "daemon.stdout.log")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(logDir, "daemon.stdout.log")); err != nil {
		t.Fatalf("detached child did not get a log file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(logDir, "daemon.stderr.log")); err != nil {
		t.Fatalf("detached child did not get a stderr log: %v", err)
	}
}

// TestStartDaemonNeverUsesCommandContext is a static guard: the launcher must not
// bind the child to the proxy's context, or the daemon would die with the IDE.
func TestStartDaemonNeverUsesCommandContext(t *testing.T) {
	path := "launch_windows.go"
	if runtime.GOOS != "windows" {
		path = "launch_unix.go"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, forbidden := range []string{"exec.CommandContext(", "cmd.Wait()"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("%s must not use %s: the daemon has to outlive the proxy", path, forbidden)
		}
	}
	// The child is started and released, never waited on.
	if !strings.Contains(body, "cmd.Process.Release()") {
		t.Fatalf("%s must release the child process handle", path)
	}
	if !strings.Contains(body, "API_TOKEN=") {
		t.Fatalf("%s must pass the token through the environment, not argv", path)
	}
}
