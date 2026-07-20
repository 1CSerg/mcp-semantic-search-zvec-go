package chunk

import "github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/ast"

// CloseResources releases native tree-sitter handles allocated by the AST
// chunker. It is process-shutdown only and a no-op when tree-sitter is not
// compiled in. Callers must ensure no other goroutine is parsing when invoked.
func CloseResources() {
	ast.CloseResources()
}
