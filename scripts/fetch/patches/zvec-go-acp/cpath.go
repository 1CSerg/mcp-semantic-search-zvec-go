package zvec

/*
#include <stdlib.h>
*/
import "C"
import "unsafe"

func cStringPath(path string) (*C.char, func(), error) {
	normalized, err := normalizeFilesystemPath(path)
	if err != nil {
		return nil, nil, err
	}
	if !containsNonASCII(normalized) {
		p := C.CString(normalized)
		return p, func() { C.free(unsafe.Pointer(p)) }, nil
	}
	native, err := nativePathBytes(normalized)
	if err != nil {
		return nil, nil, err
	}
	buf := make([]byte, len(native)+1)
	copy(buf, native)
	p := C.CBytes(buf)
	return (*C.char)(p), func() { C.free(p) }, nil
}
