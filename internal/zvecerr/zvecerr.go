package zvecerr

import "strings"

// IsLockError reports zvec collection LOCK contention errors.
func IsLockError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "lock file") || strings.Contains(msg, "can't open lock")
}

// IsCorruptSegmentError reports zvec segment corruption (e.g. "File is too small: N").
func IsCorruptSegmentError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "file is too small") ||
		strings.Contains(msg, "corrupt")
}

// IsSkippablePerFileError reports zvec failures that should skip one file without aborting the job.
// Collection-wide errors (LOCK contention) are excluded.
func IsSkippablePerFileError(err error) bool {
	if err == nil {
		return false
	}
	if IsCorruptSegmentError(err) {
		return true
	}
	if IsLockError(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "zvec error [internal_error]") {
		return false
	}
	// Only skip internal errors that indicate a single-file/segment problem.
	return strings.Contains(msg, "file is too small") ||
		strings.Contains(msg, "corrupt")
}
