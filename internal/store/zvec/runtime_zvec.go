//go:build zvec

package zvec

import (
	"sync"

	zvec "github.com/zvec-ai/zvec-go"
)

var shutdownOnce sync.Once

// ShutdownRuntime tears down the native zvec-go runtime. Safe to call multiple times.
func ShutdownRuntime() {
	shutdownOnce.Do(func() {
		zvec.Shutdown()
	})
}
