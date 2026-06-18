package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/zvecerr"
)

var errFatalEmbed = errors.New("indexing embed")

func fatalEmbedErr(err error) error {
	return fmt.Errorf("%w: %w", errFatalEmbed, err)
}

// IsContextInterrupt reports lifecycle shutdown (context.Canceled) that should not be persisted as fatal.
// HTTP client timeouts (DeadlineExceeded) are real errors and must not trigger interrupted recovery.
func IsContextInterrupt(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled)
}

func isFatalIndexingError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, errStalledRecovery) {
		return true
	}
	if errors.Is(err, errFatalEmbed) {
		return true
	}
	if errors.Is(err, zvec.ErrOwnerMismatch) {
		return true
	}
	return false
}

func isPerFileSkippable(err error) bool {
	if isFatalIndexingError(err) {
		return false
	}
	if zvecerr.IsLockError(err) {
		return false
	}
	// File-level I/O issues (vanished file, permission) affect one path only.
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return true
	}
	if errors.Is(err, zvec.ErrOwnerMismatch) {
		return false
	}
	if zvecerr.IsSkippablePerFileError(err) {
		return true
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "file too large for indexing") ||
		strings.Contains(lower, "line too long for indexing") ||
		strings.Contains(lower, "escapes workspace root") {
		return true
	}
	// Unknown failures (manifest/DB writes, unexpected store errors) abort the job
	// rather than silently skipping every file and producing a half-built index.
	return false
}
