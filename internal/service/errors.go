package service

import "errors"

// ErrIndexingInProgress is returned when search is attempted during active indexing (HTTP 409).
var ErrIndexingInProgress = errors.New("indexing in progress")
