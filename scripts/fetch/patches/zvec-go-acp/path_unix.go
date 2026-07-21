//go:build !windows

package zvec

import "path/filepath"

func normalizeFilesystemPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	return filepath.Abs(path)
}

func nativePathBytes(path string) ([]byte, error) {
	return []byte(path), nil
}

func containsNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}
