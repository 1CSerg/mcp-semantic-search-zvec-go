//go:build !windows

package lock

import "syscall"

// ProcessAlive reports whether a process with pid is currently alive.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

func processAlive(pid int) bool {
	return ProcessAlive(pid)
}
