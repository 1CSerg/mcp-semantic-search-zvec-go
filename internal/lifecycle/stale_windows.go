//go:build windows

package lifecycle

import (
	"fmt"
	"log/slog"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
	"golang.org/x/sys/windows"
)

func listStdioPIDs(workspace string, selfPID int) ([]procCandidate, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))

	var pids []procCandidate
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		pid := int(pe.ProcessID)
		name := windows.UTF16ToString(pe.ExeFile[:])
		if !strings.Contains(strings.ToLower(name), binaryName) {
			continue
		}
		cmdline, err := processCommandLine(uint32(pid))
		if err != nil {
			slog.Warn("stdio scan: read cmdline failed", "pid", pid, "err", err)
			continue
		}
		if !matchesStaleStdio(cmdline, workspace, pid, selfPID) {
			continue
		}
		pids = append(pids, procCandidate{PID: pid, StartTime: lock.ProcessStartTime(pid)})
	}
	if err != nil && err != syscall.ERROR_NO_MORE_FILES {
		return pids, err
	}
	return pids, nil
}

func listStdioPIDsForIndexDir(indexDir string, selfPID int) ([]procCandidate, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))

	var pids []procCandidate
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		pid := int(pe.ProcessID)
		name := windows.UTF16ToString(pe.ExeFile[:])
		if !strings.Contains(strings.ToLower(name), binaryName) {
			continue
		}
		cmdline, err := processCommandLine(uint32(pid))
		if err != nil {
			slog.Warn("stdio scan: read cmdline failed", "pid", pid, "err", err)
			continue
		}
		if !matchesStdioIndexDir(cmdline, indexDir, pid, selfPID) {
			continue
		}
		pids = append(pids, procCandidate{PID: pid, StartTime: lock.ProcessStartTime(pid)})
	}
	if err != nil && err != syscall.ERROR_NO_MORE_FILES {
		return pids, err
	}
	return pids, nil
}

func stopStaleStdioInstances(workspace string, selfPID int) ([]int, error) {
	cands, err := listStdioPIDs(workspace, selfPID)
	if err != nil {
		return nil, err
	}
	var stopped []int
	for _, c := range cands {
		if err := terminatePIDChecked(c.PID, c.StartTime); err != nil {
			slog.Warn("stale stdio scan: terminate failed", "pid", c.PID, "err", err)
			continue
		}
		stopped = append(stopped, c.PID)
	}
	return stopped, nil
}

func processCommandLine(pid uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(handle) }()

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
	if us.Length == 0 {
		return "", fmt.Errorf("empty command line for pid %d", pid)
	}
	n := int(us.Length / 2)
	if n <= 0 {
		return "", fmt.Errorf("empty command line for pid %d", pid)
	}
	// ProcessCommandLineInformation stores WCHARs in the same query buffer.
	// Do not dereference us.Buffer: it may be misaligned or outside this buffer.
	bufBase := uintptr(unsafe.Pointer(&buf[0]))
	bufEnd := bufBase + uintptr(len(buf))
	if us.Buffer != nil {
		ptr := uintptr(unsafe.Pointer(us.Buffer))
		if ptr >= bufBase && ptr+uintptr(n*2) <= bufEnd {
			start := int(ptr - bufBase)
			inline := buf[start : start+n*2]
			return windows.UTF16ToString(unsafe.Slice(
				(*uint16)(unsafe.Pointer(&inline[0])),
				n,
			)), nil
		}
	}
	headerSize := int(unsafe.Sizeof(windows.NTUnicodeString{}))
	if headerSize+n*2 > len(buf) {
		return "", fmt.Errorf("command line buffer too small for pid %d", pid)
	}
	inline := buf[headerSize : headerSize+n*2]
	return windows.UTF16ToString(unsafe.Slice(
		(*uint16)(unsafe.Pointer(&inline[0])),
		n,
	)), nil
}

func terminatePID(pid int, _ int64) error {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return err
	}
	time.Sleep(killGrace)
	return nil
}
