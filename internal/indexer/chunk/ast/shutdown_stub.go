//go:build !zvec || !treesitter

package ast

// CloseResources is a no-op stub when tree-sitter is unavailable.
func CloseResources() {}
