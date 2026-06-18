//go:build zvec && treesitter

package ast

import (
	"sort"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type bslRegionEvent struct {
	line    uint
	isStart bool
	name    string
}

type bslRegionTracker struct {
	events []bslRegionEvent
}

func buildBSLRegionTracker(root *sitter.Node, src []byte) *bslRegionTracker {
	if root == nil {
		return nil
	}
	var events []bslRegionEvent
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "preprocessor" {
			if hasChildKind(n, "PREPROC_REGION_KEYWORD") {
				if name := bslPreprocessorRegionName(n, src); name != "" {
					events = append(events, bslRegionEvent{
						line:    n.StartPosition().Row + 1,
						isStart: true,
						name:    name,
					})
				}
			} else if hasChildKind(n, "PREPROC_ENDREGION_KEYWORD") {
				events = append(events, bslRegionEvent{
					line:    n.StartPosition().Row + 1,
					isStart: false,
				})
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(root)
	sort.Slice(events, func(i, j int) bool {
		if events[i].line == events[j].line {
			return events[i].isStart && !events[j].isStart
		}
		return events[i].line < events[j].line
	})
	return &bslRegionTracker{events: events}
}

func hasChildKind(node *sitter.Node, kind string) bool {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child != nil && child.Kind() == kind {
			return true
		}
	}
	return false
}

func bslPreprocessorRegionName(node *sitter.Node, src []byte) string {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Kind() == "identifier" {
			return child.Utf8Text(src)
		}
	}
	return ""
}

func (t *bslRegionTracker) scopeAtLine(base Scope, line uint) Scope {
	if t == nil || len(t.events) == 0 || line == 0 {
		return base
	}
	out := base
	var stack []string
	for _, ev := range t.events {
		if ev.line >= line {
			break
		}
		if ev.isStart {
			stack = append(stack, ev.name)
		} else if len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
	}
	for _, name := range stack {
		out = out.WithSegment("region", name)
	}
	return out
}
