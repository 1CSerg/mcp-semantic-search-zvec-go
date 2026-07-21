//go:build windows

package zvec

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func normalizeFilesystemPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	long, err := longPath(abs)
	if err != nil {
		return abs, nil
	}
	return filepath.Clean(long), nil
}

func containsNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}

func nativePathBytes(path string) ([]byte, error) {
	if !containsNonASCII(path) {
		return []byte(path), nil
	}
	return utf16ToSystemACP(path)
}

func utf16ToSystemACP(path string) ([]byte, error) {
	u16, err := syscall.UTF16FromString(path)
	if err != nil {
		return nil, fmt.Errorf("utf16 from string: %w", err)
	}
	if len(u16) == 0 {
		return nil, fmt.Errorf("empty utf16 path")
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	wideCharToMultiByte := kernel32.NewProc("WideCharToMultiByte")

	const cpACP = 0
	size, _, err := wideCharToMultiByte.Call(
		uintptr(cpACP),
		0,
		uintptr(unsafe.Pointer(&u16[0])),
		uintptr(len(u16)-1),
		0,
		0,
		0,
		0,
	)
	if size == 0 {
		return nil, os.NewSyscallError("WideCharToMultiByte(size)", err)
	}

	buf := make([]byte, int(size))
	written, _, err := wideCharToMultiByte.Call(
		uintptr(cpACP),
		0,
		uintptr(unsafe.Pointer(&u16[0])),
		uintptr(len(u16)-1),
		uintptr(unsafe.Pointer(&buf[0])),
		size,
		0,
		0,
	)
	if written == 0 {
		return nil, os.NewSyscallError("WideCharToMultiByte", err)
	}
	return buf[:written], nil
}

func longPath(path string) (string, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getLongPathNameW := kernel32.NewProc("GetLongPathNameW")

	input, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", fmt.Errorf("utf16 path: %w", err)
	}

	size, _, callErr := getLongPathNameW.Call(uintptr(unsafe.Pointer(input)), 0, 0)
	if size == 0 {
		if callErr != syscall.Errno(0) {
			return "", os.NewSyscallError("GetLongPathNameW", callErr)
		}
		return path, nil
	}

	buf := make([]uint16, size)
	written, _, callErr := getLongPathNameW.Call(
		uintptr(unsafe.Pointer(input)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(size),
	)
	if written == 0 {
		if callErr != syscall.Errno(0) {
			return "", os.NewSyscallError("GetLongPathNameW", callErr)
		}
		return path, nil
	}
	return syscall.UTF16ToString(buf[:written]), nil
}
