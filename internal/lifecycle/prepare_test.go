package lifecycle

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/testutil"
)

func init() {
	flag.Bool("stdio", false, "test-only: accept --stdio in helper subprocess cmdline")
}

func staleHelperBinaryPath(dir string) string {
	name := "mcp-semantic-search-zvec-go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}

func waitForHelperReady(cmd *exec.Cmd, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	if runtime.GOOS == "windows" {
		for time.Now().Before(deadline) {
			if cmd.Process != nil {
				return nil
			}
			time.Sleep(20 * time.Millisecond)
		}
		return fmt.Errorf("helper not ready within %v", timeout)
	}
	var aliveSince time.Time
	for time.Now().Before(deadline) {
		if cmd.Process == nil {
			aliveSince = time.Time{}
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if !lock.ProcessAlive(cmd.Process.Pid) {
			aliveSince = time.Time{}
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if aliveSince.IsZero() {
			aliveSince = time.Now()
		}
		if time.Since(aliveSince) >= 200*time.Millisecond {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("helper not ready within %v", timeout)
}

func startStaleHelper(t *testing.T, workspace string) *exec.Cmd {
	t.Helper()
	installDir := filepath.Join(workspace, config.DefaultInstallDirName)
	binDir := filepath.Join(installDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	helper := staleHelperBinaryPath(binDir)
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Skipf("read test binary: %v", err)
	}
	if err := os.WriteFile(helper, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(binDir, config.WorkspaceRootMarkerFile),
		[]byte(workspace),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(helper, "-test.run=TestHelperStaleStdio", "-test.v", "--stdio", workspace)
	cmd.Env = testutil.HelperProcessEnv("GO_WANT_HELPER=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Skipf("start helper: %v", err)
	}
	if err := waitForHelperReady(cmd, 15*time.Second); err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		t.Fatalf("wait for helper: %v (stderr: %s)", err, stderr.String())
	}
	return cmd
}

func TestStopStaleStdioInstancesNoMatch(t *testing.T) {
	stopped, err := stopStaleStdioInstances(t.TempDir(), os.Getpid())
	if err != nil {
		t.Fatalf("stopStaleStdioInstances: %v", err)
	}
	_ = stopped
}

// TestHelperStaleStdio is a subprocess target for stale-process cleanup tests.
func TestHelperStaleStdio(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER") != "1" {
		return
	}
	select {}
}

func helperCmdlineMatchable(workspace string, pid int) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return false
	}
	line := strings.ReplaceAll(string(cmdline), "\x00", " ")
	return matchesStaleStdio(line, workspace, pid, os.Getpid())
}

func assertHelperCmdlineMatchable(t *testing.T, workspace string, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if helperCmdlineMatchable(workspace, pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	snippet := "(cmdline unreadable)"
	if cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline")); err == nil {
		snippet = strings.ReplaceAll(string(cmdline), "\x00", " ")
	}
	t.Fatalf("helper cmdline not matchable within %v: pid=%d snippet=%q", timeout, pid, snippet)
}

func TestStopStaleStdioKillsHelper(t *testing.T) {
	workspace := t.TempDir()
	cmd := startStaleHelper(t, workspace)
	defer func() { _ = cmd.Process.Kill() }()

	helperPID := cmd.Process.Pid
	settings := &config.Settings{
		WorkspaceRoot: workspace,
		IndexDir:      filepath.Join(workspace, config.DefaultInstallDirName, config.DefaultIndexSubdir),
	}
	if err := PrepareWorkspaceLocks(settings); err != nil {
		t.Fatalf("PrepareWorkspaceLocks: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
		return // success
	case <-time.After(5 * time.Second):
		t.Fatalf("helper pid=%d still running after PrepareWorkspaceLocks", helperPID)
	}
}

func TestPrepareStdioStopsStaleHelper(t *testing.T) {
	workspace := t.TempDir()
	cmd := startStaleHelper(t, workspace)
	defer func() { _ = cmd.Process.Kill() }()

	settings := &config.Settings{
		WorkspaceRoot: workspace,
		IndexDir:      filepath.Join(workspace, config.DefaultInstallDirName, config.DefaultIndexSubdir),
	}
	stdioLock, err := PrepareStdio(settings)
	if err != nil {
		t.Fatalf("PrepareStdio: %v", err)
	}
	defer func() { _ = stdioLock.Release() }()
	_ = cmd.Wait()
}

func TestPrepareStdio(t *testing.T) {
	dir := t.TempDir()
	settings := &config.Settings{
		WorkspaceRoot: dir,
		IndexDir:      filepath.Join(dir, config.DefaultInstallDirName, config.DefaultIndexSubdir),
	}
	stdioLock, err := PrepareStdio(settings)
	if err != nil {
		t.Fatalf("PrepareStdio: %v", err)
	}
	defer func() { _ = stdioLock.Release() }()
	logDir := settings.LogsDir()
	info, err := os.Stat(logDir)
	if err != nil {
		t.Fatalf("log dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("log dir is not a directory")
	}
}

func TestTerminatePIDNotFound(t *testing.T) {
	if err := terminatePID(99999999, 0); err == nil {
		t.Fatal("expected error for non-existent pid")
	}
}

func TestTerminatePIDChildProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "timeout", "/t", "60", "/nobreak")
		if err := cmd.Start(); err != nil {
			t.Skipf("cannot start child: %v", err)
		}
		defer func() { _ = cmd.Process.Kill() }()
		if err := terminatePID(cmd.Process.Pid, 0); err != nil {
			t.Fatalf("terminatePID: %v", err)
		}
		return
	}
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start child: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	if err := terminatePID(cmd.Process.Pid, 0); err != nil {
		t.Fatalf("terminatePID: %v", err)
	}
}

func TestPrepareStdioReclaimsStaleLockAndProgress(t *testing.T) {
	dir := t.TempDir()
	indexDir := filepath.Join(dir, config.DefaultInstallDirName, config.DefaultIndexSubdir)
	settings := &config.Settings{
		WorkspaceRoot: dir,
		IndexDir:      indexDir,
		App: config.AppConfig{
			Indexing: config.IndexingConfig{
				LockStaleSeconds: 1,
				StallSeconds:     1,
			},
		},
	}
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := indexer.NewProgressStore(indexDir)
	if err := store.Save(indexer.StartRunning(false)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "index.lock"), []byte("0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)

	stdioLock, err := PrepareStdio(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdioLock.Release() }()
	if pid, ok := lock.New(indexDir, 1).LiveHolder(); ok {
		t.Fatalf("expected stale index lock reclaimed, live pid=%d", pid)
	}
	p, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if p.Running {
		t.Fatal("expected stalled progress recovered")
	}
}

func TestPrepareStdioSingletonLock(t *testing.T) {
	dir := t.TempDir()
	settings := &config.Settings{
		WorkspaceRoot: dir,
		IndexDir:      filepath.Join(dir, config.DefaultInstallDirName, config.DefaultIndexSubdir),
		App: config.AppConfig{
			Indexing: config.IndexingConfig{
				LockStaleSeconds: 300,
			},
		},
	}
	first, err := PrepareStdio(settings)
	if err != nil {
		t.Fatalf("first PrepareStdio: %v", err)
	}
	defer func() { _ = first.Release() }()

	_, err = PrepareStdio(settings)
	if err == nil {
		t.Fatal("expected error when stdio lock already held")
	}
}

func TestPrepareStdioReclaimsZvecLock(t *testing.T) {
	dir := t.TempDir()
	indexDir := filepath.Join(dir, config.DefaultInstallDirName, config.DefaultIndexSubdir)
	settings := &config.Settings{
		WorkspaceRoot: dir,
		IndexDir:      indexDir,
		App: config.AppConfig{
			Indexing: config.IndexingConfig{
				LockStaleSeconds: 300,
			},
		},
	}
	collectionPath := filepath.Join(indexDir, "zvec", "ws_testcollection")
	if err := os.MkdirAll(collectionPath, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(collectionPath, "LOCK")
	if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdioLock, err := PrepareStdio(settings)
	if err != nil {
		t.Fatalf("PrepareStdio: %v", err)
	}
	defer func() { _ = stdioLock.Release() }()
	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("expected reclaimed zvec LOCK path to exist: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected truncated zvec LOCK, size=%d", info.Size())
	}
}
