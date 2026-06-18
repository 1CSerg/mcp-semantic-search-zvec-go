package ast

import "strings"

// Scope is a chain of segments serialized as parent_scope (e.g. "package auth > type Server > func Auth").
type Scope struct {
	Segments []ScopeSegment
}

// ScopeSegment is one named scope level.
type ScopeSegment struct {
	Kind string // package, type, class, function, method, ...
	Name string
}

// String renders scope for parent_scope metadata and context prefix.
func (s Scope) String() string {
	if len(s.Segments) == 0 {
		return ""
	}
	parts := make([]string, 0, len(s.Segments))
	for _, seg := range s.Segments {
		if seg.Name == "" {
			continue
		}
		switch seg.Kind {
		case "package":
			parts = append(parts, "package "+seg.Name)
		case "type":
			parts = append(parts, "type "+seg.Name)
		case "func":
			parts = append(parts, "func "+seg.Name)
		case "method":
			parts = append(parts, "method "+seg.Name)
		default:
			parts = append(parts, seg.Kind+" "+seg.Name)
		}
	}
	return strings.Join(parts, " > ")
}

// PackageScope builds the root package segment.
func PackageScope(name string) Scope {
	if name == "" {
		return Scope{}
	}
	return Scope{Segments: []ScopeSegment{{Kind: "package", Name: name}}}
}

// WithSegment returns a copy with an additional segment appended.
func (s Scope) WithSegment(kind, name string) Scope {
	if name == "" {
		return s
	}
	out := Scope{Segments: append([]ScopeSegment(nil), s.Segments...)}
	out.Segments = append(out.Segments, ScopeSegment{Kind: kind, Name: name})
	return out
}
