package lifecycle

import "github.com/1CSerg/mcp-semantic-search-zvec-go/internal/zvecerr"

// IsZvecCorruptSegmentError reports zvec segment corruption (e.g. "File is too small: N").
func IsZvecCorruptSegmentError(err error) bool {
	return zvecerr.IsCorruptSegmentError(err)
}

// IsZvecSkippablePerFileError reports zvec failures that should skip one file without aborting the job.
func IsZvecSkippablePerFileError(err error) bool {
	return zvecerr.IsSkippablePerFileError(err)
}

// IsPerFileRecoverable reports errors that should skip a single file without aborting the job.
func IsPerFileRecoverable(err error) bool {
	return zvecerr.IsSkippablePerFileError(err)
}
