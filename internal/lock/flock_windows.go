//go:build windows

package lock

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockExclusiveNB(f *os.File) error {
	var ol windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&ol,
	)
	if err != nil {
		if err == windows.ERROR_LOCK_VIOLATION {
			return errLockHeld
		}
		return err
	}
	return nil
}

func unlock(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}
