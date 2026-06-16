package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

var errFatalEmbed = errors.New("indexing embed")

func fatalEmbedErr(err error) error {
	return fmt.Errorf("%w: %w", errFatalEmbed, err)
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
	// File-level I/O issues (vanished file, permission) affect one path only.
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return true
	}
	if errors.Is(err, zvec.ErrOwnerMismatch) {
		return false
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "file too large for indexing") ||
		strings.Contains(lower, "line too long for indexing") ||
		strings.Contains(lower, "file is too small") ||
		strings.Contains(lower, "corrupt") ||
		strings.Contains(lower, "zvec error") {
		return true
	}
	// Unknown failures (manifest/DB writes, unexpected store errors) abort the job
	// rather than silently skipping every file and producing a half-built index.
	return false
}
