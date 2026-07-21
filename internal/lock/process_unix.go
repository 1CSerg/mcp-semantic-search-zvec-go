//go:build !windows

package lock

import "syscall"

// ProcessAlive reports whether a process with pid is currently alive.
// Zombie processes count as not alive: syscall.Kill(pid, 0) succeeds for
// unreaped children, so we filter them via processIsZombie. StartTime checks
// in processMatchesLock remain the defense against PID reuse.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err != nil {
		return false
	}
	return !processIsZombie(pid)
}

func processAlive(pid int) bool {
	return ProcessAlive(pid)
}
