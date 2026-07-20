//go:build zvec && treesitter

package ast

// CloseResources releases native tree-sitter handles (C TSParser and TSQuery
// objects) allocated by this package. It is process-shutdown only: callers must
// ensure no other goroutine is parsing. The tree-sitter Go bindings publish no
// finalizers for these handles, so without this call they leak C memory.
func CloseResources() {
	closeResources()
}
