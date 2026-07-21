//go:build linux

package lock

import (
	"fmt"
	"os"
)

// processIsZombie reports whether pid is a Linux zombie (state 'Z' in
// /proc/<pid>/stat). If /proc cannot be read, returns false (fail-open) so a
// live holder is not reclaimed under restricted environments.
func processIsZombie(pid int) bool {
	if pid <= 0 {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	return linuxStatIsZombie(string(data))
}
