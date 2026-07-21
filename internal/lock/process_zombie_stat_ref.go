//go:build !linux

package lock

// Keep the shared /proc/stat parsers referenced on non-linux GOOS so that
// unit tests can exercise them everywhere while golangci unused (tests: false)
// does not flag them when process_zombie_linux.go is excluded from the build.
var _ = linuxStatIsZombie
var _ = parseStatState
