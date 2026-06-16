//go:build darwin

package lock

import "golang.org/x/sys/unix"

func processStartUnix(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0
	}
	return kp.Proc.P_starttime.Sec
}
