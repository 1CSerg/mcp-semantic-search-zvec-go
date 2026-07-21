package lifecycle

import (
	"fmt"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

// procCandidate is a PID together with the unix start time captured at scan
// time. Carrying the start time through to terminatePID lets us re-check
// identity immediately before signalling, defending against pid reuse in the
// window between listing /proc (or the Windows snapshot) and kill.
type procCandidate struct {
	PID       int
	StartTime int64 // unix start time; 0 if unavailable on this platform
}

// sameLiveProcess reports whether pid currently refers to the process that was
// alive at scan time with recordedStart. If the start time cannot be determined
// (recordedStart <= 0, or the platform lacks start-time support) it falls back
// to a pure liveness check — the historical behaviour.
func sameLiveProcess(pid int, recordedStart int64) bool {
	if !lock.ProcessAlive(pid) {
		return false
	}
	if recordedStart <= 0 {
		return true
	}
	currentStart := lock.ProcessStartTime(pid)
	if currentStart <= 0 {
		// Start time unavailable right now: trust liveness rather than risk a
		// false negative that would leave a stale process running.
		return true
	}
	// Exact equality: processStartUnix returns a stable absolute unix start
	// time (boot_time + ticks/USER_HZ on Linux, creation time on Windows/Darwin),
	// so the same live process reports the same value on every call. Any
	// difference means the pid was recycled. A loose tolerance here would let a
	// freshly-recycled pid slip through and we could signal the wrong process.
	return currentStart == recordedStart
}

// terminatePIDChecked terminates pid but only if it still identifies the process
// captured at recordedStart (defending against pid reuse). recordedStart <= 0
// falls back to the plain identity-free terminate.
func terminatePIDChecked(pid int, recordedStart int64) error {
	if !sameLiveProcess(pid, recordedStart) {
		return fmt.Errorf("process %d not found or pid reused", pid)
	}
	return terminatePID(pid, recordedStart)
}
