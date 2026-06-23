//go:build windows

package lock

import (
	"syscall"
	"time"
)

func processStartUnix(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	const processQueryLimited = 0x1000
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return 0
	}
	defer func() { _ = syscall.CloseHandle(h) }()

	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0
	}
	return creation.Nanoseconds() / int64(time.Second)
}
