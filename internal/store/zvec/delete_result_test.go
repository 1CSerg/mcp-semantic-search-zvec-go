package zvec

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// dedupeIDs must not touch the caller's slice: DeleteByIDs receives ids owned by the
// cleanup journal and the coordinator, which reuse them after the call.
func TestDedupeIDsPreservesOrderAndInput(t *testing.T) {
	in := []string{"b", "a", "b", "c", "a"}
	original := append([]string(nil), in...)

	got := dedupeIDs(in)
	want := []string{"b", "a", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dedupeIDs=%v want %v", got, want)
	}
	if !reflect.DeepEqual(in, original) {
		t.Fatalf("dedupeIDs mutated its input: %v want %v", in, original)
	}

	// The result must not alias the input's backing array.
	got[0] = "mutated"
	if in[0] != "b" {
		t.Fatalf("dedupeIDs aliased the input slice: %v", in)
	}
}

func TestDedupeIDsEmptyAndSingle(t *testing.T) {
	if got := dedupeIDs(nil); len(got) != 0 {
		t.Fatalf("dedupeIDs(nil)=%v want empty", got)
	}
	if got := dedupeIDs([]string{"only"}); !reflect.DeepEqual(got, []string{"only"}) {
		t.Fatalf("dedupeIDs single=%v", got)
	}
}

// deleteOutcome resolves the per-id result of a native delete that reported errors.
// An id that is already gone has reached the delete goal, so it belongs to Succeeded;
// ids that survived are the failures.
func TestDeleteOutcomeAllGone(t *testing.T) {
	succeeded, failed := deleteOutcome([]string{"a", "b"}, nil)
	if !reflect.DeepEqual(succeeded, []string{"a", "b"}) {
		t.Fatalf("succeeded=%v want [a b]", succeeded)
	}
	if failed != 0 {
		t.Fatalf("failed=%d want 0", failed)
	}
}

func TestDeleteOutcomeSomeRemaining(t *testing.T) {
	succeeded, failed := deleteOutcome([]string{"a", "b", "c"}, []string{"b"})
	if !reflect.DeepEqual(succeeded, []string{"a", "c"}) {
		t.Fatalf("succeeded=%v want [a c]", succeeded)
	}
	if failed != 1 {
		t.Fatalf("failed=%d want 1", failed)
	}
}

func TestDeleteOutcomeAllRemaining(t *testing.T) {
	succeeded, failed := deleteOutcome([]string{"a", "b"}, []string{"a", "b"})
	if len(succeeded) != 0 {
		t.Fatalf("succeeded=%v want empty", succeeded)
	}
	if failed != 2 {
		t.Fatalf("failed=%d want 2", failed)
	}
}

// An id missing from both lists must not be invented as either a success or a
// failure: the outcome only describes the request, so the failure count is the number
// of requested ids that survived.
func TestDeleteOutcomeIgnoresUnrequestedRemaining(t *testing.T) {
	succeeded, failed := deleteOutcome([]string{"a"}, []string{"a", "ghost"})
	if len(succeeded) != 0 {
		t.Fatalf("succeeded=%v want empty", succeeded)
	}
	if failed != 1 {
		t.Fatalf("failed=%d want 1 (only the requested id that survived)", failed)
	}
}

// A repeated delete of ids that are already gone must read as a complete success,
// which is what makes journal replay idempotent.
func TestDeleteOutcomeRepeatDeleteSucceeds(t *testing.T) {
	err := deleteError("delete", []string{"a", "b"}, nil, nil)
	if err != nil {
		t.Fatalf("deleteError with no survivors=%v want nil", err)
	}

	var partial *PartialWriteError
	err = deleteError("delete", []string{"a"}, []string{"a"}, nil)
	if !errors.As(err, &partial) {
		t.Fatalf("deleteError with survivors=%v want *PartialWriteError", err)
	}
	if len(partial.Succeeded) != 0 {
		t.Fatalf("partial.Succeeded=%v want empty", partial.Succeeded)
	}
	if partial.Failed != 1 {
		t.Fatalf("partial.Failed=%d want 1", partial.Failed)
	}
}

// A failed Fetch must not be reported as success, and must not invent per-id results.
func TestDeleteErrorFetchFailureIsNotSuccess(t *testing.T) {
	fetchErr := errors.New("fetch exploded")
	err := deleteError("delete", []string{"a", "b"}, nil, fetchErr)
	if err == nil {
		t.Fatal("deleteError with a Fetch failure returned nil")
	}
	if !errors.Is(err, fetchErr) {
		t.Fatalf("err=%v want wrap of %v", err, fetchErr)
	}
	var partial *PartialWriteError
	if !errors.As(err, &partial) {
		t.Fatalf("err=%v want *PartialWriteError", err)
	}
	if len(partial.Succeeded) != 0 {
		t.Fatalf("Fetch failure invented successes: %v", partial.Succeeded)
	}
	if partial.Failed != 2 {
		t.Fatalf("partial.Failed=%d want 2", partial.Failed)
	}
}

// The rendered message must not repeat the operation name: PartialWriteError.Error
// already prepends it, and a doubled prefix reads like a nested error.
func TestDeleteErrorMessageDoesNotRepeatOp(t *testing.T) {
	err := deleteError("delete", []string{"a"}, []string{"a"}, nil)
	if err == nil {
		t.Fatal("want an error for a surviving id")
	}
	if got := strings.Count(err.Error(), "delete"); got != 1 {
		t.Fatalf("operation name appears %d times in %q", got, err.Error())
	}

	fetchErr := errors.New("fetch exploded")
	err = deleteError("delete", []string{"a"}, nil, fetchErr)
	if err == nil {
		t.Fatal("want an error for a failed Fetch")
	}
	if got := strings.Count(err.Error(), "delete"); got != 1 {
		t.Fatalf("operation name appears %d times in %q", got, err.Error())
	}
	if !errors.Is(err, fetchErr) {
		t.Fatalf("err=%v must wrap the fetch error", err)
	}
}
