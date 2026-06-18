package ast

import "strings"

// BoundaryMeta describes a tree-sitter boundary node for cAST.
type BoundaryMeta struct {
	Kind     string
	Name     string
	Captures map[string]string
}

// extendScope adds a segment when descending into an oversized boundary node.
// BoundaryMeta is the adapter from tree-sitter query captures (indexBoundaries);
// language-specific capture interpretation lives here (Go receiver normalization, etc.).
func extendScope(current Scope, meta BoundaryMeta, lang string) Scope {
	if meta.Kind == "" {
		return current
	}
	switch meta.Kind {
	case "function":
		if lang == "go" {
			return current.WithSegment("func", meta.Name)
		}
		return current.WithSegment("function", meta.Name)
	case "method":
		out := current
		if recv := normalizeReceiverType(meta.Captures["scope.receiver"]); recv != "" {
			out = out.WithSegment("type", recv)
		}
		return out.WithSegment("method", meta.Name)
	case "type":
		return current.WithSegment("type", meta.Name)
	case "class":
		return current.WithSegment("class", meta.Name)
	case "const":
		return current.WithSegment("const", meta.Name)
	case "var":
		return current.WithSegment("var", meta.Name)
	case "import":
		return current.WithSegment("import", meta.Name)
	default:
		return current.WithSegment(meta.Kind, meta.Name)
	}
}

// normalizeReceiverType strips pointer prefix from Go receiver type text.
func normalizeReceiverType(receiverType string) string {
	return strings.TrimPrefix(strings.TrimSpace(receiverType), "*")
}

// parentScopeForBoundary returns parent_scope for a boundary symbol.
func parentScopeForBoundary(scope Scope, meta BoundaryMeta, lang string) string {
	if meta.Kind == "method" {
		if recv := normalizeReceiverType(meta.Captures["scope.receiver"]); recv != "" {
			ps := scope
			ps = ps.WithSegment("type", recv)
			return ps.String()
		}
		if lang == "javascript" || lang == "typescript" || lang == "tsx" {
			return classScopeOnly(scope)
		}
		return scope.String()
	}
	return scope.String()
}

func classScopeOnly(scope Scope) string {
	for i := len(scope.Segments) - 1; i >= 0; i-- {
		seg := scope.Segments[i]
		if seg.Kind == "class" && seg.Name != "" {
			return "class " + seg.Name
		}
	}
	return scope.String()
}

func firstCaptureName(captures map[string]string) string {
	if v := captures["name"]; v != "" {
		return v
	}
	return ""
}

// boundaryKindFromCapture maps @boundary.* capture names to symbol_kind.
func boundaryKindFromCapture(captureName string) string {
	switch captureName {
	case "boundary.function":
		return "function"
	case "boundary.method":
		return "method"
	case "boundary.type":
		return "type"
	case "boundary.class":
		return "class"
	case "boundary.interface":
		return "interface"
	case "boundary.type_alias":
		return "type_alias"
	case "boundary.enum":
		return "enum"
	case "boundary.const":
		return "const"
	case "boundary.var":
		return "var"
	case "boundary.assignment", "boundary.expression":
		return "module_var"
	default:
		if strings.HasPrefix(captureName, "boundary.") {
			return strings.TrimPrefix(captureName, "boundary.")
		}
		return ""
	}
}
