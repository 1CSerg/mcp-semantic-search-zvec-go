//go:build !zvec || !treesitter

package ast

import (
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
)

// ChunkGo is a stub when tree-sitter is unavailable.
func ChunkGo(_ string, _ []byte, _ Config, _ token.TokenCounter, _ EmitFunc) error {
	return ErrNotImplemented
}

// ChunkLanguage is a stub when tree-sitter is unavailable.
func ChunkLanguage(_ string, _ string, _ []byte, _ Config, _ token.TokenCounter, _ EmitFunc) error {
	return ErrNotImplemented
}

// ChunkPython is a stub when tree-sitter is unavailable.
func ChunkPython(_ string, _ []byte, _ Config, _ token.TokenCounter, _ EmitFunc) error {
	return ErrNotImplemented
}

// ChunkJavaScript is a stub when tree-sitter is unavailable.
func ChunkJavaScript(_ string, _ []byte, _ Config, _ token.TokenCounter, _ EmitFunc) error {
	return ErrNotImplemented
}

// ChunkTypeScript is a stub when tree-sitter is unavailable.
func ChunkTypeScript(_ string, _ []byte, _ Config, _ token.TokenCounter, _ EmitFunc) error {
	return ErrNotImplemented
}

// ChunkTSX is a stub when tree-sitter is unavailable.
func ChunkTSX(_ string, _ []byte, _ Config, _ token.TokenCounter, _ EmitFunc) error {
	return ErrNotImplemented
}
