//go:build !windows

package lifecycle

import "testing"

func TestParentPIDFromProcStat(t *testing.T) {
	if got := parentPIDFromProcStat("123 (cmd with spaces) S 456 789"); got != 456 {
		t.Fatalf("parentPIDFromProcStat=%d, want 456", got)
	}
	if got := parentPIDFromProcStat("bad stat"); got != 0 {
		t.Fatalf("parentPIDFromProcStat invalid=%d, want 0", got)
	}
}
