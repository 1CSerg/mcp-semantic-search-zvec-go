//go:build zvec && treesitter

package ast

import (
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func emitBSLQueryInjections(root *sitter.Node, src []byte, rel string, cfg Config, counter token.TokenCounter, baseScope Scope, tracker *bslRegionTracker, emit EmitFunc) error {
	if root == nil {
		return nil
	}
	var walk func(*sitter.Node) error
	walk = func(n *sitter.Node) error {
		if n == nil {
			return nil
		}
		if n.Kind() == "string" || n.Kind() == "multiline_string" {
			raw := n.Utf8Text(src)
			cleaned := StripBSLQueryString(raw)
			if looksLikeSDBLQuery(cleaned) {
				line := int64(n.StartPosition().Row) + 1
				scope := baseScope
				if tracker != nil {
					scope = tracker.scopeAtLine(baseScope, n.StartPosition().Row+1)
				}
				if err := ChunkSDBLText(rel, cleaned, line, cfg, counter, scope.String(), emit); err != nil {
					return err
				}
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			if err := walk(n.NamedChild(i)); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}

func bslScopeForNode(base Scope, tracker *bslRegionTracker, node *sitter.Node) Scope {
	if tracker == nil || node == nil {
		return base
	}
	return tracker.scopeAtLine(base, node.StartPosition().Row+1)
}

func isBSLRegionEndPreprocessor(node *sitter.Node) bool {
	return node != nil && node.Kind() == "preprocessor" && hasChildKind(node, "PREPROC_ENDREGION_KEYWORD")
}

func isBSLRegionStartPreprocessor(node *sitter.Node) bool {
	return node != nil && node.Kind() == "preprocessor" && hasChildKind(node, "PREPROC_REGION_KEYWORD")
}

func bslRegionNameFromPreprocessor(node *sitter.Node, src []byte) string {
	if !isBSLRegionStartPreprocessor(node) {
		return ""
	}
	return strings.TrimSpace(bslPreprocessorRegionName(node, src))
}
