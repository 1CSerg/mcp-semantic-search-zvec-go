//go:build !windows

package lifecycle

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func defaultParentPID(pid int) int {
	if pid <= 0 {
		return 0
	}
	if pid == os.Getpid() {
		return os.Getppid()
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0
	}
	return parentPIDFromProcStat(string(data))
}

func defaultProcessName(pid int) string {
	if pid <= 0 {
		return ""
	}
	if data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm")); err == nil {
		return strings.TrimSpace(string(data))
	}
	if data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline")); err == nil {
		line := strings.ReplaceAll(string(data), "\x00", " ")
		fields := strings.Fields(line)
		if len(fields) > 0 {
			return filepath.Base(fields[0])
		}
	}
	return ""
}

func parentPIDFromProcStat(stat string) int {
	end := strings.LastIndex(stat, ")")
	if end < 0 || end+2 >= len(stat) {
		return 0
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 2 {
		return 0
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return ppid
}
