package indexer

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	msg := err.Error()
	if strings.Contains(msg, "index_owner_mismatch") {
		return true
	}
	return false
}

func isPerFileSkippable(err error) bool {
	if isFatalIndexingError(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "file is too small") ||
		strings.Contains(msg, "corrupt") ||
		strings.Contains(msg, "zvec error") {
		return true
	}
	// Read, chunk, and other per-file failures should not abort the whole job.
	return true
}
