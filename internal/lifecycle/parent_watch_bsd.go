//go:build !windows && !linux && !darwin

package lifecycle

import "os"

// defaultParentPID resolves only the immediate parent of this process via
// os.Getppid(). Other ancestors cannot be resolved portably on BSDs without
// /proc or a kinfo sysctl, so they report 0 and the launch chain stops at the
// direct parent. This keeps parent-watch functional for the common stdio
// launcher (which is the direct parent) while degrading gracefully elsewhere.
func defaultParentPID(pid int) int {
	if pid <= 0 {
		return 0
	}
	if pid == os.Getpid() {
		return os.Getppid()
	}
	return 0
}

func defaultProcessName(pid int) string {
	if pid == os.Getpid() {
		return "self"
	}
	return ""
}
