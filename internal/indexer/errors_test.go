package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestIsPerFileSkippable(t *testing.T) {
	skippable := []error{
		os.ErrNotExist,
		os.ErrPermission,
		fmt.Errorf("zvec error [INTERNAL_ERROR]: File is too small: 6"),
		errors.New("segment is corrupt"),
	}
	for _, err := range skippable {
		if !isPerFileSkippable(err) {
			t.Fatalf("expected skippable: %v", err)
		}
	}

	fatal := []error{
		context.Canceled,
		context.DeadlineExceeded,
		fatalEmbedErr(errors.New("boom")),
		errors.New("index_owner_mismatch: ..."),
		errors.New("manifest get foo: database is locked"),
		errors.New("some unexpected store failure"),
	}
	for _, err := range fatal {
		if isPerFileSkippable(err) {
			t.Fatalf("expected fatal (non-skippable): %v", err)
		}
	}
}
