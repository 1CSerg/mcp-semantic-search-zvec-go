package zvecerr

import (
	"errors"
	"testing"
)

func TestIsCorruptSegmentError(t *testing.T) {
	if IsCorruptSegmentError(nil) {
		t.Fatal("nil should be false")
	}
	if !IsCorruptSegmentError(errors.New("File is too small: 12")) {
		t.Fatal("expected corrupt segment")
	}
	if !IsCorruptSegmentError(errors.New("segment CORRUPT")) {
		t.Fatal("expected corrupt keyword")
	}
	if IsCorruptSegmentError(errors.New("other failure")) {
		t.Fatal("unexpected match")
	}
}

func TestIsLockError(t *testing.T) {
	if IsLockError(nil) {
		t.Fatal("nil should not be lock error")
	}
	if !IsLockError(errors.New(`zvec error [INTERNAL_ERROR]: Can't open lock file: /tmp/ws/LOCK`)) {
		t.Fatal("expected lock file error")
	}
	if !IsLockError(errors.New(`zvec error [INTERNAL_ERROR]: Can't lock read-only collection: /tmp/ws/LOCK`)) {
		t.Fatal("expected read-only collection lock error")
	}
	if IsLockError(errors.New("collection not found")) {
		t.Fatal("unexpected lock classification")
	}
	if !IsLockError(errors.New(`can't open lock file`)) {
		t.Fatal("expected lowercase lock phrase")
	}
}

func TestIsSkippablePerFileError(t *testing.T) {
	if IsSkippablePerFileError(nil) {
		t.Fatal("nil should be false")
	}
	if !IsSkippablePerFileError(errors.New("File is too small: 8")) {
		t.Fatal("expected skippable corrupt segment")
	}
	if !IsSkippablePerFileError(errors.New(`zvec error [INTERNAL_ERROR]: Invalid: File is too small: 6`)) {
		t.Fatal("expected skippable internal_error")
	}
	if IsSkippablePerFileError(errors.New(`zvec error [INTERNAL_ERROR]: Can't open lock file: /tmp/ws/LOCK`)) {
		t.Fatal("lock errors must not be skippable per-file")
	}
	if IsSkippablePerFileError(errors.New(`zvec error [INTERNAL_ERROR]: Can't lock read-only collection: /tmp/ws/LOCK`)) {
		t.Fatal("read-only collection lock errors must not be skippable per-file")
	}
	if IsSkippablePerFileError(errors.New("zvec error: upsert failed")) {
		t.Fatal("generic zvec error without internal_error should not match")
	}
	if IsSkippablePerFileError(errors.New(`zvec error [INTERNAL_ERROR]: upsert failed`)) {
		t.Fatal("generic internal_error without per-file hint should not match")
	}
	if !IsSkippablePerFileError(errors.New(`zvec error [internal_error]: segment file is too small: 4`)) {
		t.Fatal("expected captured internal_error fixture")
	}
	if IsSkippablePerFileError(errors.New(`zvec error [internal_error]: Can't open lock file`)) {
		t.Fatal("lock internal_error must not be skippable")
	}
}
