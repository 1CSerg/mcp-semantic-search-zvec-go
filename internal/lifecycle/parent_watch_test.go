package lifecycle

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

func resetParentWatchTestHooks(t *testing.T) {
	t.Helper()
	oldInterval := parentWatchInterval
	oldCurrentPID := parentWatchCurrentPID
	oldParentPID := parentWatchParentPID
	oldProcessName := parentWatchProcessName
	oldProcessAlive := parentWatchProcessAlive
	oldProcessStart := parentWatchProcessStart
	t.Cleanup(func() {
		parentWatchInterval = oldInterval
		parentWatchCurrentPID = oldCurrentPID
		parentWatchParentPID = oldParentPID
		parentWatchProcessName = oldProcessName
		parentWatchProcessAlive = oldProcessAlive
		parentWatchProcessStart = oldProcessStart
		resetParentWatchOnceForTest()
	})
}

func TestLaunchChainProcesses(t *testing.T) {
	resetParentWatchTestHooks(t)

	parentWatchCurrentPID = func() int { return 100 }
	parentWatchParentPID = func(pid int) int {
		switch pid {
		case 100:
			return 200
		case 200:
			return 300
		default:
			return 0
		}
	}
	parentWatchProcessName = func(pid int) string {
		switch pid {
		case 200:
			return "powershell.exe"
		case 300:
			return "Cursor.exe"
		default:
			return ""
		}
	}

	chain := launchChainProcesses()
	if len(chain) != 2 {
		t.Fatalf("len(chain)=%d, want 2: %#v", len(chain), chain)
	}
	if chain[0].PID != 200 || chain[0].Name != "powershell.exe" {
		t.Fatalf("parent=%#v, want powershell pid 200", chain[0])
	}
	if chain[1].PID != 300 || chain[1].Name != "Cursor.exe" {
		t.Fatalf("grandparent=%#v, want Cursor pid 300", chain[1])
	}
}

func TestLaunchChainProcessesSkipsGrandparentWithoutShell(t *testing.T) {
	resetParentWatchTestHooks(t)

	parentWatchCurrentPID = func() int { return 100 }
	parentWatchParentPID = func(pid int) int {
		if pid == 100 {
			return 200
		}
		return 300
	}
	parentWatchProcessName = func(pid int) string {
		if pid == 200 {
			return "Cursor.exe"
		}
		return "explorer.exe"
	}

	chain := launchChainProcesses()
	if len(chain) != 1 {
		t.Fatalf("len(chain)=%d, want 1: %#v", len(chain), chain)
	}
	if chain[0].PID != 200 || chain[0].Name != "Cursor.exe" {
		t.Fatalf("parent=%#v, want Cursor pid 200", chain[0])
	}
}

func TestStartParentWatchDisabled(t *testing.T) {
	resetParentWatchTestHooks(t)
	t.Setenv(disableParentWatchEnv, "true")

	called := false
	parentWatchCurrentPID = func() int {
		called = true
		return 100
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartParentWatch(ctx, cancel)

	if called {
		t.Fatal("parent watch inspected process chain while disabled")
	}
}

func TestStartParentWatchDeadParent(t *testing.T) {
	resetParentWatchTestHooks(t)

	parentWatchCurrentPID = func() int { return 100 }
	parentWatchParentPID = func(pid int) int { return 200 }
	parentWatchProcessName = func(pid int) string { return "powershell.exe" }
	parentWatchProcessAlive = func(pid int) bool { return false }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopped := make(chan struct{})
	StartParentWatch(ctx, func() {
		cancel()
		close(stopped)
	})

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("parent watch did not stop after detecting dead parent")
	}
}

func TestProcessAliveExport(t *testing.T) {
	if !lock.ProcessAlive(os.Getpid()) {
		t.Fatal("ProcessAlive(current pid)=false")
	}
}
