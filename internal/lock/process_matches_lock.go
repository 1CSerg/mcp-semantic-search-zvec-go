package lock

func processMatchesLock(pid int, startTime int64) bool {
	if !processAlive(pid) {
		return false
	}
	if startTime <= 0 {
		return true
	}
	got := processStartUnix(pid)
	if got <= 0 {
		// Cannot verify start time; treat as non-match so stale locks are reclaimed.
		return startTime <= 0
	}
	return got == startTime
}
