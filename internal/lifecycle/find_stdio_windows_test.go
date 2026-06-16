//go:build windows

package lifecycle

import (
	"os"
	"testing"
	"time"
)

func TestFindStdioForWorkspaceSelfExcluded(t *testing.T) {
	pid, ok := FindStdioForWorkspace(t.TempDir(), 0)
	if ok || pid != 0 {
		t.Fatalf("FindStdioForWorkspace()=%d ok=%v, want none", pid, ok)
	}
}

func TestFindStdioForWorkspaceLiveProcess(t *testing.T) {
	workspace := t.TempDir()
	child := StartTestStdioHelper(t, workspace)
	defer func() { _ = child.Process.Kill() }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if pid, ok := FindStdioForWorkspace(workspace, os.Getpid()); ok && pid != os.Getpid() {
			if pid == child.Process.Pid {
				return
			}
			t.Fatalf("FindStdioForWorkspace()=%d, want child pid %d", pid, child.Process.Pid)
		}
		if time.Now().After(deadline) {
			t.Fatal("live --stdio helper not detected")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
