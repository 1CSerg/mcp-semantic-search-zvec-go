package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
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
		fmt.Errorf("%w: details", zvec.ErrOwnerMismatch),
		errors.New("manifest get foo: database is locked"),
		errors.New("some unexpected store failure"),
		errors.New(`zvec error [INTERNAL_ERROR]: Can't open lock file: /tmp/ws/LOCK`),
		errors.New("zvec error: upsert failed"),
	}
	for _, err := range fatal {
		if isPerFileSkippable(err) {
			t.Fatalf("expected fatal (non-skippable): %v", err)
		}
	}
}

func TestIsContextInterrupt(t *testing.T) {
	if !IsContextInterrupt(context.Canceled) {
		t.Fatal("expected Canceled to be interrupt")
	}
	if IsContextInterrupt(context.DeadlineExceeded) {
		t.Fatal("DeadlineExceeded must not be treated as lifecycle interrupt")
	}
	if IsContextInterrupt(fatalEmbedErr(context.DeadlineExceeded)) {
		t.Fatal("wrapped DeadlineExceeded must not be treated as interrupt")
	}
}

func TestIsFatalIndexingErrorOwnerMismatch(t *testing.T) {
	err := fmt.Errorf("%w: index belongs to workspace_id %q, current is %q", zvec.ErrOwnerMismatch, "ws-a", "ws-b")
	if !isFatalIndexingError(err) {
		t.Fatal("expected fatal owner mismatch")
	}
	if !errors.Is(err, zvec.ErrOwnerMismatch) {
		t.Fatal("expected ErrOwnerMismatch in chain")
	}
}
