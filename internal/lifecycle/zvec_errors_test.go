package lifecycle

import (
	"errors"
	"testing"
)

func TestIsZvecCorruptSegmentError(t *testing.T) {
	if IsZvecCorruptSegmentError(nil) {
		t.Fatal("nil should be false")
	}
	if !IsZvecCorruptSegmentError(errors.New("File is too small: 12")) {
		t.Fatal("expected corrupt segment")
	}
	if !IsZvecCorruptSegmentError(errors.New("segment CORRUPT")) {
		t.Fatal("expected corrupt keyword")
	}
	if IsZvecCorruptSegmentError(errors.New("other failure")) {
		t.Fatal("unexpected match")
	}
}

func TestIsPerFileRecoverable(t *testing.T) {
	if IsPerFileRecoverable(nil) {
		t.Fatal("nil should be false")
	}
	if !IsPerFileRecoverable(errors.New("File is too small: 8")) {
		t.Fatal("expected recoverable corrupt segment")
	}
	if !IsPerFileRecoverable(errors.New("zvec error: upsert failed")) {
		t.Fatal("expected recoverable zvec error")
	}
	if IsPerFileRecoverable(errors.New("fatal embed failure")) {
		t.Fatal("unexpected recoverable")
	}
}
