//go:build darwin

package lifecycle

import (
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

func defaultParentPID(pid int) int {
	if pid <= 0 {
		return 0
	}
	if pid == os.Getpid() {
		return os.Getppid()
	}
	// macOS has no /proc; resolve the parent pid via the kern.proc.pid sysctl.
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0
	}
	return int(kp.Eproc.Ppid)
}

func defaultProcessName(pid int) string {
	if pid <= 0 {
		return ""
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return ""
	}
	// Comm is a fixed-size NUL-padded array; trim trailing NULs.
	comm := kp.Proc.P_comm
	end := 0
	for end < len(comm) && comm[end] != 0 {
		end++
	}
	name := string(comm[:end])
	if name == "" {
		return strconv.Itoa(pid)
	}
	return name
}
