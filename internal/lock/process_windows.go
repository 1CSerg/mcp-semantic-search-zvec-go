//go:build windows

package lock

import "syscall"

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const processQueryLimited = 0x1000
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = syscall.CloseHandle(h)
	return true
}
