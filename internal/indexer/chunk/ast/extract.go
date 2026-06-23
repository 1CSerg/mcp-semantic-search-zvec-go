package ast

// BoundaryMeta describes a tree-sitter boundary node for cAST.
type BoundaryMeta struct {
	Kind     string
	Name     string
	Captures map[string]string
}
