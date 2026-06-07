//go:build windows

package lifecycle

import (
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func stopStaleStdioInstances(workspace string, selfPID int) ([]int, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))

	var stopped []int
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		pid := int(pe.ProcessID)
		name := windows.UTF16ToString(pe.ExeFile[:])
		if !strings.Contains(strings.ToLower(name), binaryName) {
			continue
		}
		cmdline, err := processCommandLine(uint32(pid))
		if err != nil {
			continue
		}
		if !matchesStaleStdio(cmdline, workspace, pid, selfPID) {
			continue
		}
		if err := terminatePID(pid); err != nil {
			continue
		}
		stopped = append(stopped, pid)
	}
	if err != nil && err != syscall.ERROR_NO_MORE_FILES {
		return stopped, err
	}
	return stopped, nil
}

func processCommandLine(pid uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)

	const bufSize = 8192
	buf := make([]byte, bufSize)
	if err := windows.NtQueryInformationProcess(
		handle,
		windows.ProcessCommandLineInformation,
		unsafe.Pointer(&buf[0]),
		bufSize,
		nil,
	); err != nil {
		return "", err
	}

	us := (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0]))
	if us.Buffer == nil || us.Length == 0 {
		return "", fmt.Errorf("empty command line for pid %d", pid)
	}
	n := int(us.Length / 2)
	return windows.UTF16ToString(unsafe.Slice(us.Buffer, n)), nil
}

func terminatePID(pid int) error {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return err
	}
	time.Sleep(killGrace)
	return nil
}
