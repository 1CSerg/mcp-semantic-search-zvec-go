//go:build windows

package lifecycle

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func defaultParentPID(pid int) int {
	entry, ok := processEntry(pid)
	if !ok {
		return 0
	}
	return int(entry.ParentProcessID)
}

func defaultProcessName(pid int) string {
	entry, ok := processEntry(pid)
	if !ok {
		return ""
	}
	return strings.TrimSpace(windows.UTF16ToString(entry.ExeFile[:]))
}

func processEntry(pid int) (windows.ProcessEntry32, bool) {
	if pid <= 0 {
		return windows.ProcessEntry32{}, false
	}

	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return windows.ProcessEntry32{}, false
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if int(pe.ProcessID) == pid {
			return pe, true
		}
	}
	return windows.ProcessEntry32{}, false
}
