package lifecycle

import (
	"strings"
)

// IsZvecCorruptSegmentError reports zvec segment corruption (e.g. "File is too small: N").
func IsZvecCorruptSegmentError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "file is too small") ||
		strings.Contains(msg, "corrupt")
}

// IsPerFileRecoverable reports errors that should skip a single file without aborting the job.
func IsPerFileRecoverable(err error) bool {
	if err == nil {
		return false
	}
	if IsZvecCorruptSegmentError(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "zvec error") {
		return true
	}
	return false
}
