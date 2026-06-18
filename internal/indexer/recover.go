package indexer

import (
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

// RecoverStalledProgress resets persisted running state when lock is absent or stale.
// hasLivePeer, when non-nil, can suppress recovery while another live indexer peer exists
// (for example a --stdio MCP process for the same workspace).
func RecoverStalledProgress(indexDir string, stallSeconds float64, hasLivePeer func() bool) error {
	if stallSeconds <= 0 {
		stallSeconds = 120
	}
	store := NewProgressStore(indexDir)
	p, err := store.Load()
	if err != nil || !p.Running {
		return err
	}
	l := lock.New(indexDir, stallSeconds)
	if !l.ReclaimStale() {
		if _, ok := l.LiveHolder(); ok {
			return nil
		}
		if hasLivePeer != nil && hasLivePeer() {
			return nil
		}
		if l.IsLocked() {
			return nil
		}
	}
	p = FinishError(p, errStalledRecovery)
	return store.Save(p)
}

// RecoverInterruptedProgress migrates legacy error progress from GUI/process shutdown to idle interrupted state.
func RecoverInterruptedProgress(indexDir string) error {
	store := NewProgressStore(indexDir)
	p, err := store.Load()
	if err != nil {
		return err
	}
	if !p.Running && p.State == StateError && isLegacyContextCanceledProgressError(p.Error) {
		return store.Save(FinishInterrupted(p))
	}
	return nil
}

var errStalledRecovery = stalledError{}

type stalledError struct{}

func (stalledError) Error() string { return "recovered stale indexing progress" }

// StallWatcher aborts indexing when progress stops updating.
type StallWatcher struct {
	stall time.Duration
	last  time.Time
}

// NewStallWatcher creates a stall detector from seconds.
func NewStallWatcher(stallSeconds float64) *StallWatcher {
	if stallSeconds <= 0 {
		stallSeconds = 120
	}
	now := time.Now()
	return &StallWatcher{
		stall: time.Duration(stallSeconds * float64(time.Second)),
		last:  now,
	}
}

// Touch records progress activity.
func (s *StallWatcher) Touch() {
	s.last = time.Now()
}

// Check returns an error when no progress was recorded within stall window.
func (s *StallWatcher) Check() error {
	if time.Since(s.last) > s.stall {
		return errStalledRecovery
	}
	return nil
}
