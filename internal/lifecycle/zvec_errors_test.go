package lifecycle

import (
	"errors"
	"testing"
)

func TestIsZvecCorruptSegmentError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New(`zvec error [INTERNAL_ERROR]: Invalid: File is too small: 6`), true},
		{errors.New("segment corrupt"), true},
		{errors.New("collection not found"), false},
	}
	for _, tc := range cases {
		if got := IsZvecCorruptSegmentError(tc.err); got != tc.want {
			t.Fatalf("IsZvecCorruptSegmentError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestIsPerFileRecoverable(t *testing.T) {
	if !IsPerFileRecoverable(errors.New(`zvec error [INTERNAL_ERROR]: Invalid: File is too small: 6`)) {
		t.Fatal("expected zvec upsert error to be per-file recoverable")
	}
	if IsPerFileRecoverable(errors.New("index_owner_mismatch")) {
		t.Fatal("owner mismatch should not be per-file recoverable via this helper")
	}
}
