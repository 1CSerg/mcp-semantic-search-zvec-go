package lock

import "time"

func processMatchesLock(pid int, startTime int64) bool {
	if !processAlive(pid) {
		return false
	}
	if startTime <= 0 {
		return true
	}
	got := processStartUnix(pid)
	if got <= 0 {
		return true
	}
	diff := got - startTime
	if diff < 0 {
		diff = -diff
	}
	return diff <= int64(time.Second)
}
