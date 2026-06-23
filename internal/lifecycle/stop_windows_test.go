//go:build windows

package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

func TestStopStdioForWorkspaceUsesDefaultIndexDir(t *testing.T) {
	workspace := t.TempDir()
	stopped, err := StopStdioForWorkspace(workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 0 {
		t.Fatalf("stopped=%v, want none", stopped)
	}
}

func TestStopStdioForWorkspaceReclaimsStaleLocks(t *testing.T) {
	workspace := t.TempDir()
	indexDir := filepath.Join(workspace, config.DefaultInstallDirName, config.DefaultIndexSubdir)
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	deadPID := os.Getpid() + 100000
	payload := strings.Join([]string{strconv.Itoa(deadPID), "1", "1"}, " ") + "\n"
	for _, name := range []string{lock.StdioLockFileName, "index.lock"} {
		if err := os.WriteFile(filepath.Join(indexDir, name), []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	stopped, err := StopStdioForWorkspace(workspace, indexDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 0 {
		t.Fatalf("stopped=%v, want none", stopped)
	}
	for _, name := range []string{lock.StdioLockFileName, "index.lock"} {
		if _, err := os.Stat(filepath.Join(indexDir, name)); !os.IsNotExist(err) {
			t.Fatalf("stale lock %q still present: err=%v", name, err)
		}
	}
}

func TestRecoverDuplicateStdioNoStale(t *testing.T) {
	settings := &config.Settings{WorkspaceRoot: t.TempDir()}
	if err := RecoverDuplicateStdio(settings); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverDuplicateStdioWithLiveHelper(t *testing.T) {
	workspace := t.TempDir()
	child := StartTestStdioHelper(t, workspace)
	defer func() { _ = child.Process.Kill() }()

	settings := &config.Settings{WorkspaceRoot: workspace}
	if err := RecoverDuplicateStdio(settings); err != nil {
		t.Fatal(err)
	}
}

func TestWatchParentsDetectsExit(t *testing.T) {
	resetParentWatchTestHooks(t)
	parentWatchInterval = 10 * time.Millisecond

	var alive atomic.Bool
	alive.Store(true)
	parentWatchProcessAlive = func(int) bool { return alive.Load() }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopped := make(chan struct{}, 1)
	go watchParents(ctx, func() {
		cancel()
		stopped <- struct{}{}
	}, []watchedProcess{{PID: 4242, Name: "test.exe"}})

	time.Sleep(150 * time.Millisecond)
	alive.Store(false)

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("watchParents did not stop after parent exit")
	}
}

func TestProcessEntryCurrentProcess(t *testing.T) {
	entry, ok := processEntry(os.Getpid())
	if !ok {
		t.Fatal("processEntry(current pid)=false")
	}
	if int(entry.ProcessID) != os.Getpid() {
		t.Fatalf("ProcessID=%d, want %d", entry.ProcessID, os.Getpid())
	}
	name := defaultProcessName(os.Getpid())
	if name == "" {
		t.Fatal("defaultProcessName returned empty")
	}
	parentPID := defaultParentPID(os.Getpid())
	if parentPID <= 0 {
		t.Fatalf("defaultParentPID=%d, want live parent", parentPID)
	}
}

func TestStopLauncherWrappersNoMatch(t *testing.T) {
	stopped, err := stopLauncherWrappers(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 0 {
		t.Fatalf("stopped=%v, want none", stopped)
	}
}

func TestForceReclaimLocksEmptyDir(t *testing.T) {
	if err := forceReclaimLocks(""); err != nil {
		t.Fatal(err)
	}
}
