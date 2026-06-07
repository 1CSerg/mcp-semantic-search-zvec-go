package zvec

import (
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// matchPathGlob returns true when path matches glob (empty glob matches all).
func matchPathGlob(filePath, glob string) bool {
	glob = strings.TrimSpace(glob)
	if glob == "" {
		return true
	}
	// Normalize to forward slashes for glob matching.
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	pattern := strings.ReplaceAll(glob, "\\", "/")
	ok, err := doublestar.Match(pattern, normalized)
	if err == nil && ok {
		return true
	}
	if !strings.Contains(pattern, "/") {
		ok, err = doublestar.Match(pattern, path.Base(normalized))
		return err == nil && ok
	}
	return false
}
