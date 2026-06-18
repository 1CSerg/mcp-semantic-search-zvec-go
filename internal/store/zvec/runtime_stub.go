//go:build !zvec

package zvec

// ShutdownRuntime is a no-op when zvec CGO is not linked.
func ShutdownRuntime() {}
