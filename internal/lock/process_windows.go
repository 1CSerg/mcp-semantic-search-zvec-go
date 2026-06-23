//go:build windows

package lock

import "syscall"

// ProcessAlive reports whether a process with pid is currently alive.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const processQueryLimited = 0x1000
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	var exitCode uint32
	if err := syscall.GetExitCodeProcess(h, &exitCode); err != nil {
		return true
	}
	const stillActive = 259 // STILL_ACTIVE
	return exitCode == stillActive
}

func processAlive(pid int) bool {
	return ProcessAlive(pid)
}
