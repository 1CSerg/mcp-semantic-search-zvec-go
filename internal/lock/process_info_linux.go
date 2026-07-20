//go:build linux

package lock

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// clockTicksPerSec is POSIX USER_HZ on Linux. On x86/x86_64 and essentially all
// mainstream ARM configurations the kernel exports 100. x/sys/unix does not
// expose SC_CLK_TCK on Linux (Sysconf exists only on Solaris), and reading it
// would require cgo; 100 matches `getconf CLK_TCK` on every supported distro.
const clockTicksPerSec = int64(100)

// procBootTime returns the system boot time in unix seconds parsed from the
// `btime` line of /proc/stat. This is absolute (not uptime), so combining it
// with a process' starttime tick count yields a stable start unix-time that does
// not drift as the process ages. The value is cached because /proc/stat is the
// same for the lifetime of the kernel session.
var (
	bootTimeOnce sync.Once
	bootTime     int64
)

func procBootTime() int64 {
	bootTimeOnce.Do(func() {
		bootTime = readBootTime()
	})
	return bootTime
}

func readBootTime() int64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "btime ") {
			continue
		}
		btime, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64)
		if err != nil || btime <= 0 {
			return 0
		}
		return btime
	}
	return 0
}

func processStartUnix(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	startTicks, ok := parseStatStartTime(string(data))
	if !ok {
		return 0
	}
	btime := procBootTime()
	if btime <= 0 {
		return 0
	}
	// Absolute unix start time of the process. Unlike `uptime - ticks/clk`
	// (which grows as uptime advances and breaks identity comparison within
	// seconds), this is invariant for the lifetime of the process.
	return btime + startTicks/clockTicksPerSec
}

// parseStatStartTime extracts the starttime field (the 22nd field in 1-based
// man-proc(5) numbering) from a /proc/<pid>/stat record. The second field `comm`
// is wrapped in parentheses and may contain spaces or closing parens, so we
// split on the LAST ')' rather than naively tokenizing the whole line.
func parseStatStartTime(stat string) (int64, bool) {
	end := strings.LastIndex(stat, ")")
	if end < 0 || end+1 >= len(stat) {
		return 0, false
	}
	// After ')' the fields are: state ppid pgrp session tty_nr tpgid flags
	// minflt cminflt majflt cmajflt utime stime cutime cstime priority nice
	// num_threads itrealvalue starttime ... — starttime is the 20th field here
	// (state == field 1, 0-indexed).
	fields := strings.Fields(stat[end+1:])
	const starttimeField = 19 // 0-indexed within the post-`)` slice
	if len(fields) <= starttimeField {
		return 0, false
	}
	n, err := strconv.ParseInt(fields[starttimeField], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
