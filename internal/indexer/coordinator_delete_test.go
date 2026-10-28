package indexer

import (
	"errors"
	"fmt"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

// staleDeleteZvec is a store whose DeleteByIDs always returns a fixed error, so the
// retry loop in deleteStaleVectorsFrom can be exercised for each error shape.
type staleDeleteZvec struct {
	*memZvec
	err      error
	attempts int
}

func (s *staleDeleteZvec) DeleteByIDs(ids []string) error {
	s.attempts++
	if s.err != nil {
		return s.err
	}
	return s.memZvec.DeleteByIDs(ids)
}

// A delete whose durability flush failed is NOT a completed delete. Reporting it as
// success marks the file indexed while the stale vectors are not durable, and it
// bypasses the retry loop that exists for exactly this case.
func TestDeleteStaleVectorsFlushFailureIsRetriedNotSucceeded(t *testing.T) {
	ids := []string{"a", "b"}
	store := &staleDeleteZvec{
		memZvec: newMemZvec(),
		err: &zvec.FlushWriteError{
			Op:        "delete",
			Succeeded: append([]string(nil), ids...),
			Cause:     errors.New("flush failed"),
		},
	}

	err := deleteStaleVectorsFrom(store, ids)
	if err == nil {
		t.Fatal("deleteStaleVectorsFrom reported success for a failed flush")
	}
	if !errors.Is(err, store.err) {
		t.Fatalf("err=%v want the flush error %v", err, store.err)
	}
	if store.attempts != 3 {
		t.Fatalf("attempts=%d want 3 (the retry loop must run)", store.attempts)
	}
}

// A genuine partial delete still shrinks the retry set: the ids the store confirmed as
// deleted must not be retried, and the loop stops once nothing is left.
func TestDeleteStaleVectorsPartialShrinksRetrySet(t *testing.T) {
	store := &staleDeleteZvec{
		memZvec: newMemZvec(),
		err: &zvec.PartialWriteError{
			Op:        "delete",
			Succeeded: []string{"a"},
			Failed:    1,
			Cause:     errors.New("b survived"),
		},
	}

	err := deleteStaleVectorsFrom(store, []string{"a", "b"})
	if err == nil {
		t.Fatal("deleteStaleVectorsFrom reported success while b survived")
	}
	if store.attempts != 3 {
		t.Fatalf("attempts=%d want 3", store.attempts)
	}
}

// A partial error that confirms every requested id is gone is a success: the loop must
// stop without spending its remaining attempts.
func TestDeleteStaleVectorsPartialCoveringRequestSucceeds(t *testing.T) {
	store := &staleDeleteZvec{
		memZvec: newMemZvec(),
		err: &zvec.PartialWriteError{
			Op:        "delete",
			Succeeded: []string{"a", "b"},
			Failed:    0,
			Cause:     errors.New("noise"),
		},
	}

	if err := deleteStaleVectorsFrom(store, []string{"a", "b"}); err != nil {
		t.Fatalf("deleteStaleVectorsFrom: %v", err)
	}
	if store.attempts != 1 {
		t.Fatalf("attempts=%d want 1", store.attempts)
	}
}

// A native batch error carries no per-id information, so it must be retried as a whole.
func TestDeleteStaleVectorsNativeErrorIsRetried(t *testing.T) {
	store := &staleDeleteZvec{
		memZvec: newMemZvec(),
		err:     fmt.Errorf("native batch failed"),
	}

	if err := deleteStaleVectorsFrom(store, []string{"a"}); err == nil {
		t.Fatal("deleteStaleVectorsFrom reported success for a native batch error")
	}
	if store.attempts != 3 {
		t.Fatalf("attempts=%d want 3", store.attempts)
	}
}

// A store that cannot delete because zvec is not linked is not a failure to retry.
func TestDeleteStaleVectorsNotLinkedIsSuccess(t *testing.T) {
	store := &staleDeleteZvec{memZvec: newMemZvec(), err: zvec.ErrNotLinked}

	if err := deleteStaleVectorsFrom(store, []string{"a"}); err != nil {
		t.Fatalf("deleteStaleVectorsFrom: %v", err)
	}
	if store.attempts != 1 {
		t.Fatalf("attempts=%d want 1", store.attempts)
	}
}
