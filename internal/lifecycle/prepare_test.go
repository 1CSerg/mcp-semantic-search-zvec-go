package lifecycle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/testutil"
)

func staleHelperBinaryPath(dir string) string {
	name := "mcp-semantic-search-zvec-go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}

func waitForHelperReady(cmd *exec.Cmd, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cmd.Process == nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if runtime.GOOS == "windows" {
			return nil
		}
		pid := strconv.Itoa(cmd.Process.Pid)
		if _, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline")); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("helper not ready within %v", timeout)
}

func startStaleHelper(t *testing.T, workspace string) *exec.Cmd {
	t.Helper()
	helper := staleHelperBinaryPath(workspace)
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Skipf("read test binary: %v", err)
	}
	if err := os.WriteFile(helper, data, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(helper, "-test.run=TestHelperStaleStdio", "-test.v", "--stdio", workspace)
	cmd.Env = testutil.HelperProcessEnv("GO_WANT_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Skipf("start helper: %v", err)
	}
	if err := waitForHelperReady(cmd, 5*time.Second); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("wait for helper: %v", err)
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

func TestStopStaleStdioKillsHelper(t *testing.T) {
	workspace := t.TempDir()
	cmd := startStaleHelper(t, workspace)
	defer func() { _ = cmd.Process.Kill() }()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		stopped, err := stopStaleStdioInstances(workspace, os.Getpid())
		if err != nil {
			t.Fatalf("stopStaleStdioInstances: %v", err)
		}
		if len(stopped) > 0 {
			_ = cmd.Wait()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("expected stale helper process to be stopped")
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
	if err := terminatePID(99999999); err == nil {
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
		if err := terminatePID(cmd.Process.Pid); err != nil {
			t.Fatalf("terminatePID: %v", err)
		}
		return
	}
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start child: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	if err := terminatePID(cmd.Process.Pid); err != nil {
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
	if l := lock.New(indexDir, 1); l.IsLocked() {
		t.Fatal("expected stale lock reclaimed")
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
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale zvec LOCK reclaimed: %v", err)
	}
}
