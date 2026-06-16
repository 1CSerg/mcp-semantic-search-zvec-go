//go:build !windows && !linux && !darwin

package lock

func processStartUnix(pid int) int64 {
	return 0
}
