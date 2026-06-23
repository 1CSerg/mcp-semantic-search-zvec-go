package lifecycle

import (
	"errors"
	"testing"
)

func TestLifecycleZvecWrappersDelegateToZvecerr(t *testing.T) {
	lockErr := errors.New(`zvec error [INTERNAL_ERROR]: Can't open lock file: /tmp/ws/LOCK`)
	readOnlyLockErr := errors.New(`zvec error [INTERNAL_ERROR]: Can't lock read-only collection: /tmp/ws/LOCK`)
	skippable := errors.New(`zvec error [INTERNAL_ERROR]: File is too small: 6`)
	generic := errors.New("zvec error: upsert failed")
	fatalInternal := errors.New(`zvec error [INTERNAL_ERROR]: upsert failed`)

	if !IsZvecLockError(lockErr) || !IsZvecLockError(readOnlyLockErr) || IsZvecLockError(generic) {
		t.Fatal("IsZvecLockError wrapper mismatch")
	}
	if IsZvecSkippablePerFileError(lockErr) || IsZvecSkippablePerFileError(readOnlyLockErr) {
		t.Fatal("lock must not be skippable")
	}
	if !IsZvecSkippablePerFileError(skippable) || IsZvecSkippablePerFileError(generic) || IsZvecSkippablePerFileError(fatalInternal) {
		t.Fatal("IsZvecSkippablePerFileError wrapper mismatch")
	}
	if !IsZvecCorruptSegmentError(errors.New("File is too small: 8")) {
		t.Fatal("IsZvecCorruptSegmentError wrapper mismatch")
	}
	if !IsPerFileRecoverable(skippable) {
		t.Fatal("IsPerFileRecoverable wrapper mismatch")
	}
	if IsPerFileRecoverable(skippable) != IsZvecSkippablePerFileError(skippable) {
		t.Fatal("IsPerFileRecoverable must match IsZvecSkippablePerFileError")
	}
}

func TestErrStdioScanUnsupported(t *testing.T) {
	if ErrStdioScanUnsupported == nil || ErrStdioScanUnsupported.Error() == "" {
		t.Fatal("expected sentinel error")
	}
}
