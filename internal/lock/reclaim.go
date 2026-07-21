package lock

import (
	"os"
)

// ReclaimOrphanedFile clears lockPath when no process holds an OS-level advisory lock.
func ReclaimOrphanedFile(lockPath string) bool {
	if _, err := os.Stat(lockPath); err != nil {
		return false
	}
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	if err := lockExclusiveNB(f); err != nil {
		_ = f.Close()
		return false
	}
	if err := f.Truncate(0); err != nil {
		_ = unlock(f)
		_ = f.Close()
		return false
	}
	_ = unlock(f)
	_ = f.Close()
	return true
}
