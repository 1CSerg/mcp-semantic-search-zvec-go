//go:build zvec && treesitter

package ast

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

//go:embed queries/go.scm
var goQuerySource string

//go:embed queries/python.scm
var pythonQuerySource string

//go:embed queries/javascript.scm
var javascriptQuerySource string

//go:embed queries/typescript.scm
var typescriptQuerySource string

//go:embed queries/tsx.scm
var tsxQuerySource string

//go:embed queries/bsl.scm
var bslQuerySource string

type boundaryEntry struct {
	node    sitter.Node
	capture string
	meta    BoundaryMeta
}

type grammarSpec struct {
	language   *sitter.Language
	querySrc   string
	query      *sitter.Query
	queryOnce  sync.Once
	queryErr   error
	pool       *parserPool
	scopeRoots func(root *sitter.Node, src []byte, relPath string) Scope
}

var grammars = map[string]*grammarSpec{}

func registerGrammar(name string, lang *sitter.Language, querySrc string, scopeRoots func(*sitter.Node, []byte, string) Scope) {
	grammars[name] = &grammarSpec{
		language:   lang,
		querySrc:   querySrc,
		pool:       newParserPool(lang),
		scopeRoots: scopeRoots,
	}
}

func init() {
	registerGrammar("go", goLang(), goQuerySource, goScopeFromRoot)
	registerGrammar("python", pythonLang(), pythonQuerySource, moduleScopeFromPath)
	registerGrammar("javascript", javascriptLang(), javascriptQuerySource, moduleScopeFromPath)
	registerGrammar("typescript", typescriptLang(), typescriptQuerySource, moduleScopeFromPath)
	registerGrammar("tsx", tsxLang(), tsxQuerySource, moduleScopeFromPath)
	registerGrammar("bsl", bslLang(), bslQuerySource, moduleScopeFromPath)
}

func goScopeFromRoot(root *sitter.Node, src []byte, _ string) Scope {
	spec := grammars["go"]
	if spec == nil {
		return Scope{}
	}
	query, err := spec.loadQuery()
	if err != nil {
		return Scope{}
	}
	_, scope, err := indexBoundariesWithCachedQuery(query, root, src, "go", "")
	if err == nil && scope.String() != "" {
		return scope
	}
	return Scope{}
}

func moduleScopeFromPath(_ *sitter.Node, _ []byte, relPath string) Scope {
	base := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	return ModuleScope(base)
}

func (g *grammarSpec) loadQuery() (*sitter.Query, error) {
	g.queryOnce.Do(func() {
		q, qerr := sitter.NewQuery(g.language, g.querySrc)
		if qerr != nil {
			g.queryErr = fmt.Errorf("%s at %d:%d", qerr.Message, qerr.Row, qerr.Column)
			return
		}
		g.query = q
	})
	return g.query, g.queryErr
}

func indexBoundaries(root *sitter.Node, src []byte, lang, relPath string) (map[uintptr]BoundaryMeta, Scope, error) {
	spec, ok := grammars[lang]
	if !ok {
		return nil, Scope{}, fmt.Errorf("unknown language %q", lang)
	}
	return indexBoundariesForSpec(spec, root, src, lang, relPath)
}

func indexBoundariesForSpec(spec *grammarSpec, root *sitter.Node, src []byte, lang, relPath string) (map[uintptr]BoundaryMeta, Scope, error) {
	query, err := spec.loadQuery()
	if err != nil {
		return nil, Scope{}, err
	}
	boundaries, pkgScope, err := indexBoundariesWithCachedQuery(query, root, src, lang, relPath)
	if err != nil {
		return nil, Scope{}, err
	}
	if lang != "go" {
		pkgScope = spec.scopeRoots(root, src, relPath)
	}
	return boundaries, pkgScope, nil
}

func indexBoundariesWithCachedQuery(query *sitter.Query, root *sitter.Node, src []byte, lang, relPath string) (map[uintptr]BoundaryMeta, Scope, error) {
	qc := sitter.NewQueryCursor()
	defer qc.Close()

	matches := qc.Matches(query, root, src)
	var entries []boundaryEntry
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
			if lang == "python" && isPythonModuleVarBoundary(boundaryCapture) && !isModuleLevelNode(&boundaryNode) {
				continue
			}
			meta := BoundaryMeta{
				Kind:     boundaryKindFromCapture(boundaryCapture),
				Name:     firstCaptureName(captures),
				Captures: captures,
			}
			meta = RefineBoundaryMeta(&boundaryNode, meta, boundaryCapture, lang, src)
			entries = append(entries, boundaryEntry{node: boundaryNode, capture: boundaryCapture, meta: meta})
		}
	}
	entries = filterNestedBoundaryEntries(entries)
	boundaries := make(map[uintptr]BoundaryMeta, len(entries))
	for _, e := range entries {
		boundaries[e.node.Id()] = e.meta
	}
	if lang != "go" && relPath != "" && pkgScope.String() == "" {
		pkgScope = moduleScopeFromPath(root, src, relPath)
	}
	return boundaries, pkgScope, nil
}

// IndexBoundaries exposes boundary indexing for unit tests.
func IndexBoundaries(lang string, root *sitter.Node, src []byte, relPath string) (map[uintptr]BoundaryMeta, Scope, error) {
	return indexBoundaries(root, src, lang, relPath)
}

// SupportedLanguages returns registered AST language keys.
func SupportedLanguages() []string {
	out := make([]string, 0, len(grammars))
	for name := range grammars {
		out = append(out, name)
	}
	return out
}

// filterNestedBoundaryEntries removes redundant inner boundaries (decorated/export wrappers).
func filterNestedBoundaryEntries(entries []boundaryEntry) []boundaryEntry {
	if len(entries) < 2 {
		return entries
	}
	drop := make([]bool, len(entries))
	for i := range entries {
		if drop[i] {
			continue
		}
		for j := range entries {
			if i == j || drop[j] {
				continue
			}
			if shouldDropNestedBoundary(entries[i], entries[j]) {
				drop[j] = true
			}
		}
	}
	out := make([]boundaryEntry, 0, len(entries))
	for i, e := range entries {
		if !drop[i] {
			out = append(out, e)
		}
	}
	return out
}

func shouldDropNestedBoundary(outer, inner boundaryEntry) bool {
	if !nodeContains(&outer.node, &inner.node) || !isDirectChild(&outer.node, &inner.node) {
		return false
	}
	switch outer.node.Kind() {
	case "decorated_definition":
		return inner.node.Kind() == "function_definition" || inner.node.Kind() == "class_definition"
	case "export_statement":
		return isExportInnerDeclaration(inner.node.Kind())
	default:
		return false
	}
}

func isDirectChild(outer, inner *sitter.Node) bool {
	if outer == nil || inner == nil {
		return false
	}
	parent := inner.Parent()
	return parent != nil && parent.Id() == outer.Id()
}

func isExportInnerDeclaration(kind string) bool {
	switch kind {
	case "function_declaration", "class_declaration", "interface_declaration",
		"type_alias_declaration", "enum_declaration", "internal_module",
		"lexical_declaration", "variable_declaration":
		return true
	default:
		return false
	}
}

func nodeContains(outer, inner *sitter.Node) bool {
	if outer == nil || inner == nil {
		return false
	}
	if outer.Id() == inner.Id() {
		return false
	}
	if inner.StartByte() >= outer.StartByte() && inner.EndByte() <= outer.EndByte() {
		if inner.StartByte() > outer.StartByte() || inner.EndByte() < outer.EndByte() {
			return true
		}
	}
	return false
}

func isPythonModuleVarBoundary(capture string) bool {
	return capture == "boundary.assignment" || capture == "boundary.expression"
}

func isModuleLevelNode(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "module", "program", "source_file":
			return true
		case "function_definition", "class_definition", "decorated_definition",
			"method_definition", "class_body", "block", "statement_block":
			return false
		}
	}
	return false
}
