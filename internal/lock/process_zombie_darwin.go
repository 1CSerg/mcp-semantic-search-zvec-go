//go:build darwin

package lock

import "golang.org/x/sys/unix"

// darwinSZOMB is SZOMB from Darwin bsd/sys/proc.h (awaiting collection by
// parent). golang.org/x/sys/unix does not export this constant.
const darwinSZOMB int8 = 5

// processIsZombie reports whether pid is a Darwin zombie (P_stat == SZOMB).
// If kinfo cannot be read, returns false (fail-open) so a live holder is not
// reclaimed under restricted environments.
func processIsZombie(pid int) bool {
	if pid <= 0 {
		return false
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return false
	}
	return kp.Proc.P_stat == darwinSZOMB
}
