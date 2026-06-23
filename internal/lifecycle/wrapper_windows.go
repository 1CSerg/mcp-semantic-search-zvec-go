//go:build windows

package lifecycle

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"golang.org/x/sys/windows"
)

const launcherScriptName = "run-mcp-stdio.ps1"

func stopLauncherWrappers(workspace string) ([]int, error) {
	installDir := filepath.Join(workspace, config.DefaultInstallDirName)
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))

	var stopped []int
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		pid := int(pe.ProcessID)
		name := strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))
		if !strings.Contains(name, "powershell") && !strings.Contains(name, "pwsh") {
			continue
		}
		cmdline, err := processCommandLine(uint32(pid))
		if err != nil {
			slog.Warn("launcher scan: read cmdline failed", "pid", pid, "err", err)
			continue
		}
		if !matchesLauncherWrapper(cmdline, workspace, installDir) {
			continue
		}
		if err := terminatePID(pid); err != nil {
			slog.Warn("launcher scan: terminate failed", "pid", pid, "err", err)
			continue
		}
		stopped = append(stopped, pid)
	}
	if err != nil && err != syscall.ERROR_NO_MORE_FILES {
		return stopped, err
	}
	return stopped, nil
}

func matchesLauncherWrapper(cmdline, workspace, installDir string) bool {
	if !strings.Contains(strings.ToLower(cmdline), launcherScriptName) {
		return false
	}
	if pathContainsPath(cmdline, installDir) {
		return true
	}
	return pathContainsPath(cmdline, workspace)
}
