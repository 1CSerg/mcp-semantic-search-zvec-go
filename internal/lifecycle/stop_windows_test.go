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
		l := lock.NewWithName(indexDir, name, 300)
		if pid, ok := l.LiveHolder(); ok {
			t.Fatalf("stale lock %q still live pid=%d", name, pid)
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
	stopped, err := stopLauncherWrappers(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 0 {
		t.Fatalf("stopped=%v, want none", stopped)
	}
}

func TestLaunchChainPIDSetIncludesSelfAndParent(t *testing.T) {
	resetParentWatchTestHooks(t)

	selfPID := os.Getpid()
	parentPID := selfPID + 100
	parentWatchCurrentPID = func() int { return selfPID }
	parentWatchParentPID = func(pid int) int {
		if pid == selfPID {
			return parentPID
		}
		return 0
	}
	parentWatchProcessStart = func(pid int) int64 {
		// Distinct, non-zero start times for each pid.
		return int64(pid) * 1000
	}

	chain := launchChainPIDSet()
	startSelf, ok := chain[selfPID]
	if !ok {
		t.Fatalf("launchChainPIDSet missing self pid=%d", selfPID)
	}
	if startSelf != int64(selfPID)*1000 {
		t.Fatalf("self start=%d want %d", startSelf, int64(selfPID)*1000)
	}
	startParent, ok := chain[parentPID]
	if !ok {
		t.Fatalf("launchChainPIDSet missing parent pid=%d", parentPID)
	}
	if startParent != int64(parentPID)*1000 {
		t.Fatalf("parent start=%d want %d", startParent, int64(parentPID)*1000)
	}
}

func TestIsExcludedPID(t *testing.T) {
	resetParentWatchTestHooks(t)
	// Fixed start-time for the live lookup so identity checks are deterministic.
	parentWatchProcessStart = func(pid int) int64 { return 100 }

	t.Run("recorded zero falls back to pid-only match", func(t *testing.T) {
		exclude := map[int]int64{42: 0, 99: 0}
		if !isExcludedPID(42, exclude) {
			t.Fatal("expected pid 42 (start=0) to be excluded by pid-only")
		}
		if isExcludedPID(43, exclude) {
			t.Fatal("expected pid 43 not to be excluded")
		}
		if isExcludedPID(42, nil) {
			t.Fatal("nil exclude must not exclude any pid")
		}
	})

	t.Run("matching start time excludes", func(t *testing.T) {
		// recorded=100, current=100 → match (±1s tolerance)
		exclude := map[int]int64{42: 100}
		if !isExcludedPID(42, exclude) {
			t.Fatal("expected pid 42 to be excluded with matching start")
		}
	})

	t.Run("mismatched start time (pid reuse) is not excluded", func(t *testing.T) {
		// recorded=100, but the pid now reports start=5000 → recycled → NOT excluded.
		var reportedStart int64 = 100
		parentWatchProcessStart = func(pid int) int64 { return reportedStart }
		exclude := map[int]int64{42: 100}
		if !isExcludedPID(42, exclude) {
			t.Fatal("precondition: same start should exclude")
		}
		reportedStart = 5000
		if isExcludedPID(42, exclude) {
			t.Fatal("recycled pid with mismatched start must NOT be excluded")
		}
	})

	t.Run("current start unavailable preserves exclude", func(t *testing.T) {
		// recorded=100, current unavailable (0) → safe direction: keep exclude.
		parentWatchProcessStart = func(pid int) int64 { return 0 }
		exclude := map[int]int64{42: 100}
		if !isExcludedPID(42, exclude) {
			t.Fatal("when current start is unknown, exclude should be preserved (safe)")
		}
	})
}

func TestForceReclaimLocksEmptyDir(t *testing.T) {
	if err := forceReclaimLocks(""); err != nil {
		t.Fatal(err)
	}
}
