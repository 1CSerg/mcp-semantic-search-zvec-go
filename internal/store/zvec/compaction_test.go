//go:build zvec

package zvec

import (
	"errors"
	"testing"
	"time"
)

// countingCompactHook returns a compactHook that records how many times the
// maintenance pass ran and can be told to fail.
type countingCompactHook struct {
	calls int
	err   error
}

func (h *countingCompactHook) hook() error {
	h.calls++
	return h.err
}

func newPolicyStore(t *testing.T) (*CollectionStore, *countingCompactHook) {
	t.Helper()
	h := &countingCompactHook{}
	s := &CollectionStore{collection: "policy", path: tempDir(t)}
	s.compactHook = h.hook
	return s, h
}

// A collection with no recorded changes must never be optimized: an unchanged run
// is the common case and Optimize would rewrite the whole index for nothing.
func TestCompactCleanCollectionIsNoOp(t *testing.T) {
	s, h := newPolicyStore(t)

	if err := s.Compact(); err != nil {
		t.Fatalf("Compact on clean collection: %v", err)
	}
	if h.calls != 0 {
		t.Fatalf("Compact ran %d maintenance passes on a clean collection, want 0", h.calls)
	}
}

// A missing collection (no handle, no changes) must not be created or optimized.
func TestCompactMissingCollectionIsNoOp(t *testing.T) {
	s := &CollectionStore{collection: "missing", path: tempDir(t)}

	if err := s.Compact(); err != nil {
		t.Fatalf("Compact on missing collection: %v", err)
	}
	if s.IsOpen() {
		t.Fatal("Compact must not open or create the collection")
	}
}

// Even with a dirty flag left over from a closed handle, a store without a native
// collection has nothing to optimize — and the pending work must survive for the next
// writable session.
func TestCompactDirtyWithoutHandleIsNoOp(t *testing.T) {
	s := &CollectionStore{collection: "closed", path: tempDir(t), dirty: true, writeOps: 500}

	if err := s.Compact(); err != nil {
		t.Fatalf("Compact without handle: %v", err)
	}
	if !s.dirty {
		t.Fatal("dirty must survive a pass that had no handle")
	}
	if s.writeOps != 500 {
		t.Fatalf("writeOps=%d want 500 (a no-handle pass must not restart the interval)", s.writeOps)
	}
}

// The threshold counts native write/delete operations: 127 are below it, the 128th
// triggers exactly one pass.
func TestCompactThresholdIs128Operations(t *testing.T) {
	s, h := newPolicyStore(t)

	for i := 0; i < compactionWriteThreshold-1; i++ {
		s.writeOutcomeLocked(true, true)
	}
	if h.calls != 0 {
		t.Fatalf("maintenance ran after %d operations, want 0", compactionWriteThreshold-1)
	}
	if !s.dirty {
		t.Fatal("dirty must be set while below the threshold")
	}

	s.writeOutcomeLocked(true, true)
	if h.calls != 1 {
		t.Fatalf("maintenance ran %d times at the threshold, want 1", h.calls)
	}
}

// A successful pass clears the dirty flag and restarts the interval.
func TestCompactSuccessResetsState(t *testing.T) {
	s, h := newPolicyStore(t)
	s.dirty = true
	s.writeOps = compactionWriteThreshold + 5

	if err := s.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if h.calls != 1 {
		t.Fatalf("maintenance calls=%d want 1", h.calls)
	}
	if s.dirty {
		t.Fatal("dirty must be cleared after a successful pass")
	}
	if s.writeOps != 0 {
		t.Fatalf("writeOps=%d want 0", s.writeOps)
	}

	// A second explicit Compact on the now-clean store is a no-op.
	if err := s.Compact(); err != nil {
		t.Fatalf("second Compact: %v", err)
	}
	if h.calls != 1 {
		t.Fatalf("second Compact ran maintenance again: calls=%d", h.calls)
	}
}

// A failed pass keeps the collection dirty but restarts the interval, so the next
// attempt waits another full threshold instead of firing on every following batch.
func TestCompactErrorKeepsDirtyAndRestartsInterval(t *testing.T) {
	s, h := newPolicyStore(t)
	h.err = errors.New("native optimize failed")
	s.dirty = true
	s.writeOps = compactionWriteThreshold

	if err := s.Compact(); !errors.Is(err, h.err) {
		t.Fatalf("Compact err=%v want %v", err, h.err)
	}
	if !s.dirty {
		t.Fatal("dirty must survive a failed pass")
	}
	if s.writeOps != 0 {
		t.Fatalf("writeOps=%d want 0 after a failed pass", s.writeOps)
	}

	for i := 0; i < compactionWriteThreshold-1; i++ {
		s.writeOutcomeLocked(true, true)
	}
	if h.calls != 1 {
		t.Fatalf("maintenance retried inside the interval: calls=%d", h.calls)
	}
	s.writeOutcomeLocked(true, true)
	if h.calls != 2 {
		t.Fatalf("maintenance did not retry after a full interval: calls=%d", h.calls)
	}
}

// A periodic maintenance failure must not surface as a write error: the caller's
// durability result is the only thing UpsertChunks/DeleteByIDs report.
func TestWriteOutcomeSwallowsCompactionError(t *testing.T) {
	s, h := newPolicyStore(t)
	h.err = errors.New("native optimize failed")
	s.writeOps = compactionWriteThreshold - 1

	s.writeOutcomeLocked(true, true)
	if h.calls != 1 {
		t.Fatalf("maintenance calls=%d want 1", h.calls)
	}
	if !s.dirty {
		t.Fatal("dirty must survive the swallowed failure")
	}
}

// Outcomes without a durability flush never compact: the plan forbids running
// maintenance on the failed-flush branch.
func TestWriteOutcomeWithoutFlushDoesNotCompact(t *testing.T) {
	s, h := newPolicyStore(t)

	for i := 0; i < compactionWriteThreshold*2; i++ {
		s.writeOutcomeLocked(true, false)
	}
	if h.calls != 0 {
		t.Fatalf("maintenance ran %d times without a durability flush, want 0", h.calls)
	}
	if !s.dirty {
		t.Fatal("an ambiguous outcome must leave the collection dirty")
	}
	if s.writeOps != compactionWriteThreshold*2 {
		t.Fatalf("writeOps=%d want %d", s.writeOps, compactionWriteThreshold*2)
	}
}

// Validation failures and empty argument lists never reach a native call, so they
// must not move the counter or dirty flag.
func TestWriteOutcomeIgnoresNoChange(t *testing.T) {
	s, h := newPolicyStore(t)

	for i := 0; i < compactionWriteThreshold*2; i++ {
		s.writeOutcomeLocked(false, true)
	}
	if h.calls != 0 {
		t.Fatalf("maintenance ran %d times for no-change outcomes, want 0", h.calls)
	}
	if s.dirty || s.writeOps != 0 {
		t.Fatalf("no-change outcome changed state: dirty=%v writeOps=%d", s.dirty, s.writeOps)
	}
}

// A wipe removes the collection the counters described, so they must not carry over
// to the next build.
func TestWipeCollectionResetsMaintenanceState(t *testing.T) {
	s, h := newPolicyStore(t)
	s.closeHook = func() error { return nil }
	s.dirty = true
	s.writeOps = compactionWriteThreshold / 2

	if err := s.WipeCollection(); err != nil {
		t.Fatalf("WipeCollection: %v", err)
	}
	if s.dirty || s.writeOps != 0 {
		t.Fatalf("wipe did not reset maintenance state: dirty=%v writeOps=%d", s.dirty, s.writeOps)
	}
	if err := s.Compact(); err != nil {
		t.Fatalf("Compact after wipe: %v", err)
	}
	if h.calls != 0 {
		t.Fatalf("Compact after wipe ran maintenance %d times, want 0", h.calls)
	}
}

// A read-only handle cannot rewrite the index. Attempting it would fail on every run
// and never clear the dirty flag, producing a warning per run forever; the pass must
// wait for a writable session instead.
func TestCompactReadOnlyHandleDefersMaintenance(t *testing.T) {
	s, h := newPolicyStore(t)
	s.dirty = true
	s.writeOps = compactionWriteThreshold
	s.readOnly = true

	if err := s.Compact(); err != nil {
		t.Fatalf("Compact on a read-only handle must not error: %v", err)
	}
	if h.calls != 0 {
		t.Fatalf("maintenance ran %d times on a read-only handle, want 0", h.calls)
	}
	if !s.dirty {
		t.Fatal("dirty must be kept for the next writable session")
	}

	// Once a writable handle is available the pending work is done exactly once.
	s.readOnly = false
	if err := s.Compact(); err != nil {
		t.Fatalf("Compact after a writable open: %v", err)
	}
	if h.calls != 1 {
		t.Fatalf("maintenance calls=%d want 1", h.calls)
	}
	if s.dirty {
		t.Fatal("dirty must be cleared by the successful pass")
	}
}

// The periodic path applies the same rule: a durable write on a store that has since
// become read-only cannot compact, and must not report an error to the caller.
func TestWriteOutcomeSkipsCompactionOnReadOnlyHandle(t *testing.T) {
	s, h := newPolicyStore(t)
	s.readOnly = true
	s.writeOps = compactionWriteThreshold - 1

	s.writeOutcomeLocked(true, true)
	if h.calls != 0 {
		t.Fatalf("maintenance ran %d times on a read-only handle, want 0", h.calls)
	}
	if !s.dirty {
		t.Fatal("dirty must be kept for the next writable session")
	}
}

// Close must not race a maintenance pass over the same native handle.
func TestCompactSerializesWithClose(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	s := &CollectionStore{collection: "serialize", path: tempDir(t), dirty: true}
	s.compactHook = func() error {
		close(entered)
		<-release
		return nil
	}
	s.closeHook = func() error { return nil }

	compactDone := make(chan error, 1)
	go func() { compactDone <- s.Compact() }()
	<-entered

	closeDone := make(chan struct{})
	go func() {
		_ = s.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("Close returned while a maintenance pass held the handle")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	if err := <-compactDone; err != nil {
		t.Fatalf("Compact: %v", err)
	}
	select {
	case <-closeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not finish after the maintenance pass released the handle")
	}
}
