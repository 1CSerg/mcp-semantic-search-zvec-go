package lifecycle

import "errors"

// ErrStdioScanUnsupported is returned when the OS cannot enumerate MCP stdio processes.
var ErrStdioScanUnsupported = errors.New("stdio process scan not supported on this platform")
