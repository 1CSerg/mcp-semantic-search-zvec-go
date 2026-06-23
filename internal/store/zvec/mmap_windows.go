//go:build windows && zvec

package zvec

func collectionMmapEnabled(path string) bool {
	// Google Drive / Cyrillic paths break zvec mmap IPC segments (scalar.*.ipc → File is too small).
	return !containsNonASCII(path)
}

func containsNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}
