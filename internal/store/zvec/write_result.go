package zvec

import (
	"errors"
	"fmt"
)

// WriteResult reports per-id outcomes from a native batch write or delete.
type WriteResult struct {
	Succeeded []string
	Failed    int
}

// PartialWriteError is returned when a native batch partially succeeded.
type PartialWriteError struct {
	Op        string
	Succeeded []string
	Failed    int
	Cause     error
}

func (e *PartialWriteError) Error() string {
	if e == nil {
		return "partial write"
	}
	return fmt.Sprintf("%s: %d failed (success %d): %v", e.Op, e.Failed, len(e.Succeeded), e.Cause)
}

func (e *PartialWriteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// FlushWriteError is returned when documents may have been written but Flush failed.
type FlushWriteError struct {
	Op        string
	Succeeded []string
	Cause     error
}

func (e *FlushWriteError) Error() string {
	if e == nil {
		return "flush failed"
	}
	return fmt.Sprintf("%s flush failed after %d writes: %v", e.Op, len(e.Succeeded), e.Cause)
}

func (e *FlushWriteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// PartialWriteOutcome extracts succeeded ids and whether the error is partial/flush-ambiguous.
func PartialWriteOutcome(err error) (succeeded []string, partial bool) {
	if err == nil {
		return nil, false
	}
	var pwe *PartialWriteError
	if errors.As(err, &pwe) {
		return append([]string(nil), pwe.Succeeded...), true
	}
	var fwe *FlushWriteError
	if errors.As(err, &fwe) {
		return append([]string(nil), fwe.Succeeded...), true
	}
	return nil, false
}

// dedupeIDs returns ids without duplicates in first-seen order.
//
// It always allocates: the caller's slice belongs to the cleanup journal or the
// coordinator and is read again after the call, so it must never be reused as
// scratch space. The single-id case skips the map — rollback deletes of one id are the
// hottest caller.
func dedupeIDs(ids []string) []string {
	if len(ids) == 1 {
		return []string{ids[0]}
	}
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// deleteOutcome resolves the per-id result of a native delete that reported errors.
//
// requested is the deduplicated request; remaining lists the requested ids that
// still exist. A key that is already absent has reached the delete goal, so it counts
// as succeeded — that is what makes a repeated delete (journal replay) idempotent.
// Ids absent from both lists are neither invented as successes nor as failures.
func deleteOutcome(requested, remaining []string) (succeeded []string, failed int) {
	still := make(map[string]struct{}, len(remaining))
	for _, id := range remaining {
		still[id] = struct{}{}
	}
	succeeded = make([]string, 0, len(requested))
	for _, id := range requested {
		if _, ok := still[id]; ok {
			continue
		}
		succeeded = append(succeeded, id)
	}
	// Count only survivors that were actually requested: an id outside the request
	// must not inflate the failure count.
	failed = 0
	for _, id := range requested {
		if _, ok := still[id]; ok {
			failed++
		}
	}
	return succeeded, failed
}

// deleteError turns a native delete that reported errors into the error DeleteByIDs
// returns.
//
// remaining is what a post-delete Fetch actually found; fetchErr reports that the
// Fetch itself failed, in which case no per-id result can be claimed and the whole
// request is reported as unresolved. A nil return means every requested key is gone
// and the caller still has to flush for durability.
//
// The Cause carries no op prefix: PartialWriteError.Error already prepends it.
func deleteError(op string, requested, remaining []string, fetchErr error) error {
	if fetchErr != nil {
		return &PartialWriteError{
			Op:        op,
			Succeeded: nil,
			Failed:    len(requested),
			Cause:     fmt.Errorf("resolve remaining ids: %w", fetchErr),
		}
	}
	succeeded, failed := deleteOutcome(requested, remaining)
	if failed == 0 {
		return nil
	}
	return &PartialWriteError{
		Op:        op,
		Succeeded: succeeded,
		Failed:    failed,
		Cause:     fmt.Errorf("%d ids still present", failed),
	}
}
