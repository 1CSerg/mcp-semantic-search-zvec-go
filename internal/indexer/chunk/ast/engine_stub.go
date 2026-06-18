//go:build !zvec || !treesitter

package ast

import (
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
)

// ChunkGo is a stub when tree-sitter is unavailable.
func ChunkGo(_ string, _ []byte, _ Config, _ token.TokenCounter, _ EmitFunc) error {
	return ErrNotImplemented
}
