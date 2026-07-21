//go:build !windows && !linux && !darwin

package lock

// processIsZombie is unavailable on this platform; treat as not a zombie
// (fail-open). Kill(0) remains the sole liveness signal.
func processIsZombie(pid int) bool {
	return false
}
