package zvec

import (
	"errors"
	"fmt"
)

// WriteResult reports per-id outcomes from a native batch write or delete.
type WriteResult struct {
	Succeeded []string
	Failed    int
}

// PartialWriteError is returned when a native batch partially succeeded.
type PartialWriteError struct {
	Op        string
	Succeeded []string
	Failed    int
	Cause     error
}

func (e *PartialWriteError) Error() string {
	if e == nil {
		return "partial write"
	}
	return fmt.Sprintf("%s: %d failed (success %d): %v", e.Op, e.Failed, len(e.Succeeded), e.Cause)
}

func (e *PartialWriteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// FlushWriteError is returned when documents may have been written but Flush failed.
type FlushWriteError struct {
	Op        string
	Succeeded []string
	Cause     error
}

func (e *FlushWriteError) Error() string {
	if e == nil {
		return "flush failed"
	}
	return fmt.Sprintf("%s flush failed after %d writes: %v", e.Op, len(e.Succeeded), e.Cause)
}

func (e *FlushWriteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// PartialWriteOutcome extracts succeeded ids and whether the error is partial/flush-ambiguous.
func PartialWriteOutcome(err error) (succeeded []string, partial bool) {
	if err == nil {
		return nil, false
	}
	var pwe *PartialWriteError
	if errors.As(err, &pwe) {
		return append([]string(nil), pwe.Succeeded...), true
	}
	var fwe *FlushWriteError
	if errors.As(err, &fwe) {
		return append([]string(nil), fwe.Succeeded...), true
	}
	return nil, false
}
