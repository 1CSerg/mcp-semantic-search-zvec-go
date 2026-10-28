package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/manifest"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func fileContentHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (c *Coordinator) reconcileCleanupJournal(journal *manifest.CleanupJournal) {
	if journal == nil {
		return
	}
	ids, err := journal.Pending()
	if err != nil || len(ids) == 0 {
		return
	}
	if err := c.Zvec.DeleteByIDs(ids); err != nil && !isZvecUnavailable(err) {
		slog.Warn("cleanup journal delete failed", "count", len(ids), "err", err)
		return
	}
	_ = journal.Clear()
}

// compactStoreBestEffort runs the store's optional maintenance pass at the end of an
// indexing run.
//
// Compaction is advisory: a store without the optional zvec.Compactor (older
// implementations, test doubles) is skipped, and any error is logged without
// touching the run's result. It must run before the run publishes Running=false, so
// callers that wait for idle observe a settled index directory.
//
// A cancelled or shutting-down run does not start a new full rebuild of the index.
func compactStoreBestEffort(ctx context.Context, store zvec.Store) {
	if store == nil {
		return
	}
	compactor, ok := store.(zvec.Compactor)
	if !ok {
		return
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			slog.Info("skipping index compaction: run interrupted", "err", err)
			return
		}
	}
	if err := compactor.Compact(); err != nil {
		if isZvecUnavailable(err) {
			return
		}
		// The index is still correct, only its on-disk footprint stays large.
		slog.Warn("index compaction failed", "err", err)
	}
}

func deleteStaleVectorsFrom(store zvec.Store, ids []string) error {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := store.DeleteByIDs(ids)
		if err == nil || isZvecUnavailable(err) {
			return nil
		}
		// Only a PartialWriteError carries trustworthy per-id information: its
		// Succeeded list names the ids the store verified as gone. A FlushWriteError
		// also lists every requested id in Succeeded, but that means "written, not
		// necessarily durable" — shrinking the retry set from it would report a failed
		// flush as a completed delete and skip the retry this loop exists for.
		var partial *zvec.PartialWriteError
		if errors.As(err, &partial) && len(partial.Succeeded) > 0 {
			ids = staleDocIDs(ids, partial.Succeeded)
			if len(ids) == 0 {
				return nil
			}
		}
		lastErr = err
		slog.Warn("stale vector delete failed", "attempt", attempt, "count", len(ids), "err", err)
		time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
	}
	return lastErr
}
