package service

import "errors"

// ErrIndexingInProgress is returned by HTTPProxy when a legacy daemon responds with HTTP 409 during search.
var ErrIndexingInProgress = errors.New("indexing in progress")

// ErrShuttingDown is returned by SemanticSearch when the service is already shutting down
// and no new in-flight searches can be accepted.
var ErrShuttingDown = errors.New("service is shutting down")
