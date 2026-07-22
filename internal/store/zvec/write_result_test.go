package zvec

import (
	"errors"
	"testing"
)

func TestPartialWriteOutcome(t *testing.T) {
	err := &PartialWriteError{Op: "upsert", Succeeded: []string{"a", "b"}, Failed: 1, Cause: errors.New("native")}
	ids, partial := PartialWriteOutcome(err)
	if !partial || len(ids) != 2 {
		t.Fatalf("partial=%v ids=%v", partial, ids)
	}
	_, partial = PartialWriteOutcome(errors.New("other"))
	if partial {
		t.Fatal("expected non-partial")
	}
}
