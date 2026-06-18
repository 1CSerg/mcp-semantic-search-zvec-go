//go:build zvec && treesitter

package ast

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

//go:embed queries/go.scm
var goQuerySource string

var (
	goQuery     *sitter.Query
	goQueryErr  error
	goQueryOnce sync.Once
)

func loadGoQuery() (*sitter.Query, error) {
	goQueryOnce.Do(func() {
		q, qerr := sitter.NewQuery(goLang(), goQuerySource)
		if qerr != nil {
			goQueryErr = fmt.Errorf("%s at %d:%d", qerr.Message, qerr.Row, qerr.Column)
			return
		}
		goQuery = q
	})
	return goQuery, goQueryErr
}

// indexBoundaries maps boundary node IDs to metadata from compiled Go queries.
func indexBoundaries(root *sitter.Node, src []byte) (map[uintptr]BoundaryMeta, Scope, error) {
	query, err := loadGoQuery()
	if err != nil {
		return nil, Scope{}, fmt.Errorf("compile go query: %w", err)
	}

	qc := sitter.NewQueryCursor()
	defer qc.Close()

	matches := qc.Matches(query, root, src)
	boundaries := make(map[uintptr]BoundaryMeta)
	var pkgScope Scope

	captureNames := query.CaptureNames()
	for match := matches.Next(); match != nil; match = matches.Next() {
		captures := make(map[string]string)
		var boundaryNode sitter.Node
		var boundaryCapture string
		var hasBoundary bool

		for _, cap := range match.Captures {
			name := captureNames[cap.Index]
			text := cap.Node.Utf8Text(src)
			captures[name] = text
			if name == "scope.package" {
				pkgScope = PackageScope(text)
			}
			if strings.HasPrefix(name, "boundary.") {
				boundaryNode = cap.Node
				boundaryCapture = name
				hasBoundary = true
			}
		}
		if hasBoundary {
			kind := boundaryKindFromCapture(boundaryCapture)
			meta := BoundaryMeta{
				Kind:     kind,
				Name:     firstCaptureName(captures),
				Captures: captures,
			}
			boundaries[boundaryNode.Id()] = meta
		}
	}
	return boundaries, pkgScope, nil
}
