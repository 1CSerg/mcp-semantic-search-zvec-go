package service

import "errors"

// ErrIndexingInProgress is returned by HTTPProxy when a legacy daemon responds with HTTP 409 during search.
var ErrIndexingInProgress = errors.New("indexing in progress")
