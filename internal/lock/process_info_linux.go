//go:build linux

package lock

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func processStartUnix(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 22 {
		return 0
	}
	startTicks, err := strconv.ParseInt(fields[21], 10, 64)
	if err != nil {
		return 0
	}
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0
	}
	clockTicks := int64(100) // POSIX USER_HZ on Linux
	uptime := int64(info.Uptime)
	return uptime - (startTicks / clockTicks)
}
