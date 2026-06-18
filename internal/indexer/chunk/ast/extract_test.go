//go:build zvec && treesitter

package ast

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

func parseSample(t *testing.T, lang, rel string) (*sitter.Tree, []byte, func()) {
	t.Helper()
	path := filepath.Join("..", "testdata", lang, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pool := parserPoolForLang(lang)
	parser, tree, err := pool.parseTree(data)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		tree.Close()
		pool.release(parser)
	}
	return tree, data, cleanup
}

func findBoundaryNode(t *testing.T, lang string, root *sitter.Node, src []byte, match func(BoundaryMeta) bool) (*sitter.Node, BoundaryMeta) {
	t.Helper()
	bounds, _, err := indexBoundaries(root, src, lang, "sample."+lang)
	if err != nil {
		t.Fatal(err)
	}
	var walk func(*sitter.Node) (*sitter.Node, BoundaryMeta, bool)
	walk = func(n *sitter.Node) (*sitter.Node, BoundaryMeta, bool) {
		if n == nil {
			return nil, BoundaryMeta{}, false
		}
		if meta, ok := bounds[n.Id()]; ok && match(meta) {
			return n, meta, true
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			if node, meta, found := walk(n.NamedChild(i)); found {
				return node, meta, true
			}
		}
		return nil, BoundaryMeta{}, false
	}
	node, meta, ok := walk(root)
	if !ok {
		t.Fatal("boundary not found")
	}
	return node, meta
}

func TestExtractSymbol_Python(t *testing.T) {
	tree, src, cleanup := parseSample(t, "python", "sample.py")
	defer cleanup()
	root := tree.RootNode()

	handlerNode, handlerMeta := findBoundaryNode(t, "python", root, src, func(m BoundaryMeta) bool {
		return m.Name == "handler" && m.Kind == "function"
	})
	if handlerNode.Kind() != "decorated_definition" {
		t.Fatalf("expected decorated_definition, got %q", handlerNode.Kind())
	}
	if handlerMeta.Kind != "function" || handlerMeta.Name != "handler" {
		t.Fatalf("handler meta: %+v", handlerMeta)
	}

	_, innerMeta := findBoundaryNode(t, "python", root, src, func(m BoundaryMeta) bool {
		return m.Name == "Inner" && m.Kind == "class"
	})
	if innerMeta.Kind != "class" {
		t.Fatalf("Inner meta: %+v", innerMeta)
	}

	_, initMeta := findBoundaryNode(t, "python", root, src, func(m BoundaryMeta) bool {
		return m.Name == "__init__"
	})
	if initMeta.Kind != "method" {
		t.Fatalf("__init__ kind=%q want method", initMeta.Kind)
	}

	scope := ModuleScope("sample").WithSegment("function", "handler")
	if scope.String() != "module sample > function handler" {
		t.Fatalf("scope=%q", scope.String())
	}
}

func TestChunkPython_InnerParentScope(t *testing.T) {
	src := loadLangSampleFromPath(t, filepath.Join("..", "testdata", "python", "sample.py"))
	var chunks []zvec.Chunk
	err := ChunkPython("testdata/python/sample.py", src, testCfg(200), token.CharCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ch := range chunks {
		if ch.SymbolName == "Inner" && ch.SymbolKind == "class" {
			found = true
			want := "module sample > function handler"
			if ch.ParentScope != want {
				t.Fatalf("Inner parent_scope=%q want %q", ch.ParentScope, want)
			}
		}
	}
	if !found {
		t.Fatalf("Inner chunk not found in %+v", toGolden(chunks))
	}
}

func loadLangSampleFromPath(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExtractSymbol_JSTS(t *testing.T) {
	tree, src, cleanup := parseSample(t, "javascript", "sample.js")
	defer cleanup()
	root := tree.RootNode()

	_, arrowMeta := findBoundaryNode(t, "javascript", root, src, func(m BoundaryMeta) bool {
		return m.Name == "arrow"
	})
	if arrowMeta.Kind != "function" {
		t.Fatalf("arrow kind=%q want function", arrowMeta.Kind)
	}

	exportNode, exportMeta := findBoundaryNode(t, "javascript", root, src, func(m BoundaryMeta) bool {
		return m.Name == "DefaultService" && m.Kind == "class"
	})
	if exportNode.Kind() != "export_statement" {
		t.Fatalf("expected export_statement, got %q", exportNode.Kind())
	}
	if exportMeta.Kind != "class" {
		t.Fatalf("export meta: %+v", exportMeta)
	}

	treeTS, srcTS, cleanupTS := parseSample(t, "typescript", "sample.ts")
	defer cleanupTS()
	rootTS := treeTS.RootNode()

	ifaceNode, ifaceMeta := findBoundaryNode(t, "typescript", rootTS, srcTS, func(m BoundaryMeta) bool {
		return m.Name == "User" && m.Kind == "interface"
	})
	if ifaceNode.Kind() != "export_statement" {
		t.Fatalf("expected export_statement for interface, got %q", ifaceNode.Kind())
	}
	if ifaceMeta.Kind != "interface" {
		t.Fatalf("interface meta: %+v", ifaceMeta)
	}

	anonNode, anonMeta := findBoundaryNode(t, "typescript", rootTS, srcTS, func(m BoundaryMeta) bool {
		return m.Kind == "class" && m.Name == "AnonymousExport"
	})
	if anonNode.Kind() != "export_statement" {
		t.Fatalf("expected export_statement for default class, got %q", anonNode.Kind())
	}
	if anonMeta.Name != "AnonymousExport" {
		t.Fatalf("anonymous export name=%q", anonMeta.Name)
	}
}

func TestExtractSymbol_ArrowInline(t *testing.T) {
	src := []byte("const fn = () => 1;\n")
	pool := parserPoolForLang("javascript")
	parser, tree, err := pool.parseTree(src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	defer pool.release(parser)

	bounds, _, err := indexBoundaries(tree.RootNode(), src, "javascript", "sample.js")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, meta := range bounds {
		if meta.Name == "fn" && meta.Kind == "function" {
			found = true
		}
	}
	if !found {
		t.Fatalf("bounds=%+v", bounds)
	}
}

func TestExtractSymbol_VarDeclaration(t *testing.T) {
	src := []byte("var x = 1;\n")
	pool := parserPoolForLang("javascript")
	parser, tree, err := pool.parseTree(src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	defer pool.release(parser)
	bounds, _, err := indexBoundaries(tree.RootNode(), src, "javascript", "sample.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(bounds) == 0 {
		t.Fatal("expected boundaries")
	}
	for _, meta := range bounds {
		if meta.Name == "x" && meta.Kind != "var" && meta.Kind != "function" {
			t.Fatalf("meta=%+v", meta)
		}
	}
}

func TestParentScopeClassOnly(t *testing.T) {
	scope := ModuleScope("sample").WithSegment("class", "UserService")
	ps := parentScopeForBoundary(scope, BoundaryMeta{Kind: "method", Name: "getUser"}, "typescript")
	if ps != "class UserService" {
		t.Fatalf("got %q", ps)
	}
}

func TestSupportedLanguagesAndIndexBoundaries(t *testing.T) {
	langs := SupportedLanguages()
	if len(langs) < 5 {
		t.Fatalf("langs=%v", langs)
	}
	tree, src, cleanup := parseSample(t, "python", "sample.py")
	defer cleanup()
	bounds, scope, err := IndexBoundaries("python", tree.RootNode(), src, "sample.py")
	if err != nil {
		t.Fatal(err)
	}
	if len(bounds) == 0 || scope.String() != "module sample" {
		t.Fatalf("bounds=%d scope=%q", len(bounds), scope.String())
	}
}

func TestExtractSymbol_FunctionExpression(t *testing.T) {
	src := []byte("const fn = function() { return 1; };\n")
	pool := parserPoolForLang("javascript")
	parser, tree, err := pool.parseTree(src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	defer pool.release(parser)

	bounds, _, err := indexBoundaries(tree.RootNode(), src, "javascript", "sample.js")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, meta := range bounds {
		if meta.Name == "fn" && meta.Kind == "function" {
			found = true
		}
	}
	if !found {
		t.Fatalf("bounds=%+v", bounds)
	}
}

func TestExtractSymbol_NamespaceExport(t *testing.T) {
	src := []byte("export namespace Utils { export const x = 1; }\n")
	pool := parserPoolForLang("typescript")
	parser, tree, err := pool.parseTree(src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	defer pool.release(parser)

	bounds, _, err := indexBoundaries(tree.RootNode(), src, "typescript", "sample.ts")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, meta := range bounds {
		if meta.Name == "Utils" && meta.Kind == "namespace" {
			found = true
		}
	}
	if !found {
		t.Fatalf("bounds=%+v", bounds)
	}
}

func TestFormatTestError(t *testing.T) {
	if FormatTestError(nil) != "" {
		t.Fatal()
	}
	if FormatTestError(errParseFailed) == "" {
		t.Fatal()
	}
}
