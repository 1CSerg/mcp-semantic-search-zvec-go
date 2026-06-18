//go:build zvec && treesitter

package ast

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// extractSymbolKind maps a boundary node and query capture to unified symbol_kind and name.
func extractSymbolKind(node *sitter.Node, boundaryCapture string, captures map[string]string, src []byte, lang string) (kind, name string) {
	kind = boundaryKindFromCapture(boundaryCapture)
	name = firstCaptureName(captures)

	switch node.Kind() {
	case "decorated_definition":
		kind, name = drillDecoratedDefinition(node, src, lang)
	case "export_statement":
		kind, name = drillExportStatement(node, src, lang)
	case "lexical_declaration", "variable_declaration":
		if k, n := drillVariableDeclaration(node, src); k != "" {
			kind, name = k, n
		}
	case "function_definition":
		if lang == "python" && hasAncestorKind(node, "class_definition") {
			kind = "method"
		}
	case "method_definition":
		kind = "method"
	case "namespace_export_declaration", "internal_module":
		kind = "namespace"
		if name == "" {
			name = identifierName(node, "name", src)
		}
	}

	if boundaryCapture == "boundary.method" {
		kind = "method"
	}
	if kind == "declaration" {
		if k, n := drillVariableDeclaration(node, src); k != "" {
			kind, name = k, n
		} else if kind == "declaration" {
			kind = "const"
		}
	}
	if kind == "decorated" {
		kind, name = drillDecoratedDefinition(node, src, lang)
	}
	if kind == "export" {
		kind, name = drillExportStatement(node, src, lang)
	}

	if name == "" && kind == "class" && node.Kind() == "export_statement" {
		name = defaultExportClassName(node, src)
	}

	return kind, name
}

func drillDecoratedDefinition(node *sitter.Node, src []byte, lang string) (kind, name string) {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "function_definition":
			kind = "function"
			name = identifierName(child, "name", src)
			if lang == "python" && hasAncestorKind(child, "class_definition") {
				kind = "method"
			}
			return kind, name
		case "class_definition":
			return "class", identifierName(child, "name", src)
		}
	}
	return "", ""
}

func drillExportStatement(node *sitter.Node, src []byte, lang string) (kind, name string) {
	decl := findExportDeclaration(node)
	if decl == nil {
		return "", ""
	}
	return symbolFromDeclaration(decl, src, lang)
}

func drillVariableDeclaration(node *sitter.Node, src []byte) (kind, name string) {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil || child.Kind() != "variable_declarator" {
			continue
		}
		n := identifierName(child, "name", src)
		value := child.ChildByFieldName("value")
		if value == nil {
			for j := uint(0); j < child.NamedChildCount(); j++ {
				c := child.NamedChild(j)
				if c != nil && c.Kind() != "identifier" {
					value = c
					break
				}
			}
		}
		if value != nil && (value.Kind() == "arrow_function" || value.Kind() == "function" || value.Kind() == "function_expression") {
			return "function", n
		}
		if n != "" && kind == "" {
			kind = declarationKind(node.Kind())
			name = n
		}
	}
	return kind, name
}

func declarationKind(nodeKind string) string {
	switch nodeKind {
	case "variable_declaration":
		return "var"
	default:
		return "const"
	}
}

func findExportDeclaration(node *sitter.Node) *sitter.Node {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "function_declaration", "class_declaration", "interface_declaration",
			"type_alias_declaration", "enum_declaration", "internal_module",
			"lexical_declaration", "variable_declaration", "method_definition":
			return child
		}
	}
	return nil
}

func symbolFromDeclaration(decl *sitter.Node, src []byte, lang string) (kind, name string) {
	switch decl.Kind() {
	case "function_declaration":
		return "function", identifierName(decl, "name", src)
	case "class_declaration":
		n := typeIdentifierName(decl, "name", src)
		if n == "" {
			n = defaultExportClassName(decl, src)
		}
		return "class", n
	case "interface_declaration":
		return "interface", typeIdentifierName(decl, "name", src)
	case "type_alias_declaration":
		return "type_alias", typeIdentifierName(decl, "name", src)
	case "enum_declaration":
		return "enum", identifierName(decl, "name", src)
	case "namespace_export_declaration", "internal_module":
		return "namespace", identifierName(decl, "name", src)
	case "lexical_declaration", "variable_declaration":
		return drillVariableDeclaration(decl, src)
	default:
		return "", ""
	}
}

func defaultExportClassName(node *sitter.Node, src []byte) string {
	if n := typeIdentifierName(node, "name", src); n != "" {
		return n
	}
	if n := identifierName(node, "name", src); n != "" {
		return n
	}
	return "AnonymousExport"
}

func identifierName(node *sitter.Node, field string, src []byte) string {
	if node == nil {
		return ""
	}
	n := node.ChildByFieldName(field)
	if n != nil && (n.Kind() == "identifier" || n.Kind() == "property_identifier") {
		return n.Utf8Text(src)
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child != nil && (child.Kind() == "identifier" || child.Kind() == "property_identifier") {
			return child.Utf8Text(src)
		}
	}
	return ""
}

func typeIdentifierName(node *sitter.Node, field string, src []byte) string {
	if node == nil {
		return ""
	}
	n := node.ChildByFieldName(field)
	if n != nil {
		return n.Utf8Text(src)
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child != nil && (child.Kind() == "type_identifier" || child.Kind() == "identifier") {
			return child.Utf8Text(src)
		}
	}
	return ""
}

func hasAncestorKind(node *sitter.Node, kind string) bool {
	for p := node.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == kind {
			return true
		}
	}
	return false
}

// RefineBoundaryMeta applies drill-down and language rules to indexed boundary metadata.
func RefineBoundaryMeta(node *sitter.Node, meta BoundaryMeta, boundaryCapture string, lang string, src []byte) BoundaryMeta {
	kind, name := extractSymbolKind(node, boundaryCapture, meta.Captures, src, lang)
	if kind != "" {
		meta.Kind = kind
	}
	if name != "" {
		meta.Name = name
	}
	return meta
}
