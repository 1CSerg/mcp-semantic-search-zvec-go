package lifecycle

import (
	"errors"
	"testing"
)

func TestIsZvecLockError(t *testing.T) {
	if IsZvecLockError(nil) {
		t.Fatal("nil should not be lock error")
	}
	if !IsZvecLockError(errors.New(`zvec error [INTERNAL_ERROR]: Can't open lock file: /tmp/ws/LOCK`)) {
		t.Fatal("expected lock file error")
	}
	if IsZvecLockError(errors.New("collection not found")) {
		t.Fatal("unexpected lock classification")
	}
}
