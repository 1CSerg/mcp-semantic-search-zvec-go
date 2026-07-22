package indexer

import (
	"crypto/sha256"
	"encoding/hex"
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

func deleteStaleVectorsFrom(store zvec.Store, ids []string) error {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := store.DeleteByIDs(ids)
		if err == nil || isZvecUnavailable(err) {
			return nil
		}
		if succeeded, partial := zvec.PartialWriteOutcome(err); partial && len(succeeded) > 0 {
			ids = staleDocIDs(ids, succeeded)
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
