//go:build windows

package zvec

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func systemACPToUTF16(native []byte) (string, error) {
	if len(native) == 0 {
		return "", fmt.Errorf("empty native path")
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	multiByteToWideChar := kernel32.NewProc("MultiByteToWideChar")

	const cpACP = 0
	size, _, err := multiByteToWideChar.Call(
		uintptr(cpACP),
		0,
		uintptr(unsafe.Pointer(&native[0])),
		uintptr(len(native)),
		0,
		0,
	)
	if size == 0 {
		return "", os.NewSyscallError("MultiByteToWideChar(size)", err)
	}

	buf := make([]uint16, int(size))
	written, _, err := multiByteToWideChar.Call(
		uintptr(cpACP),
		0,
		uintptr(unsafe.Pointer(&native[0])),
		uintptr(len(native)),
		uintptr(unsafe.Pointer(&buf[0])),
		size,
	)
	if written == 0 {
		return "", os.NewSyscallError("MultiByteToWideChar", err)
	}
	return syscall.UTF16ToString(buf), nil
}
