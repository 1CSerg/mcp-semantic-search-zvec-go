//go:build zvec && treesitter

package ast

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type goldenChunk struct {
	StartLine     int64  `json:"start_line"`
	EndLine       int64  `json:"end_line"`
	SymbolName    string `json:"symbol_name"`
	SymbolKind    string `json:"symbol_kind"`
	ChunkStrategy string `json:"chunk_strategy"`
	ParentScope   string `json:"parent_scope"`
}

func testCfg(budget int) Config {
	return Config{
		MinChunkTokens:   1,
		MaxInputTokens:   budget,
		EmbedBudgetRatio: 1.0,
		WindowLines:      5,
		OverlapLines:     1,
	}
}

func collectGoChunks(t *testing.T, src []byte, budget int) []zvec.Chunk {
	t.Helper()
	var out []zvec.Chunk
	err := ChunkGo("testdata/go/sample.go", src, testCfg(budget), token.CharCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			out = append(out, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ChunkGo: %v", err)
	}
	return out
}

func toGolden(chunks []zvec.Chunk) []goldenChunk {
	out := make([]goldenChunk, 0, len(chunks))
	for _, ch := range chunks {
		out = append(out, goldenChunk{
			StartLine:     ch.StartLine,
			EndLine:       ch.EndLine,
			SymbolName:    ch.SymbolName,
			SymbolKind:    ch.SymbolKind,
			ChunkStrategy: ch.ChunkStrategy,
			ParentScope:   ch.ParentScope,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartLine != out[j].StartLine {
			return out[i].StartLine < out[j].StartLine
		}
		return out[i].SymbolName < out[j].SymbolName
	})
	return out
}

func loadSample(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "testdata", "go", "sample.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestChunkGoGolden(t *testing.T) {
	src := loadSample(t)
	chunks := collectGoChunks(t, src, 120)
	got := toGolden(chunks)
	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("..", "testdata", "go", "golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("updated golden.json")
		return
	}
	wantData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (set UPDATE_GOLDEN=1 to create): %v", err)
	}
	var want []goldenChunk
	if err := json.Unmarshal(wantData, &want); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("chunk count: got %d want %d\ngot=%s", len(got), len(want), string(data))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunk[%d]: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestChunkGoTokenBudget(t *testing.T) {
	src := loadSample(t)
	counter := token.CharCounter{}
	budget := 120
	chunks := collectGoChunks(t, src, budget)
	for _, ch := range chunks {
		if counter.Count(ch.Snippet) > budget {
			t.Fatalf("chunk exceeds budget: lines %d-%d size=%d", ch.StartLine, ch.EndLine, counter.Count(ch.Snippet))
		}
	}
}

func TestChunkGoTokenBudgetContextPrefix(t *testing.T) {
	src := []byte("package main\n\nfunc F() int {\n\treturn 42\n}\n")
	counter := token.CharCounter{}
	budget := 80
	cfg := testCfg(budget)
	cfg.ContextPrefix = true
	var chunks []zvec.Chunk
	err := ChunkGo("auth/handler.go", src, cfg, counter, func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range chunks {
		bodyBudget := cfg.bodyBudget(counter, ch.RelativePath, ch.ParentScope)
		if counter.Count(ch.Snippet) > bodyBudget {
			t.Fatalf("chunk exceeds context-adjusted budget: lines %d-%d snippet=%d budget=%d",
				ch.StartLine, ch.EndLine, counter.Count(ch.Snippet), bodyBudget)
		}
	}
}

func TestChunkGoUniqueSpans(t *testing.T) {
	src := loadSample(t)
	chunks := collectGoChunks(t, src, 120)
	seen := make(map[string]int)
	for _, ch := range chunks {
		key := fmt.Sprintf("%d:%d:%s:%s", ch.StartLine, ch.EndLine, ch.SymbolName, ch.SymbolKind)
		seen[key]++
		if seen[key] > 1 {
			t.Fatalf("duplicate chunk span %s", key)
		}
	}
}

func TestChunkGoCoverage(t *testing.T) {
	src := loadSample(t)
	chunks := collectGoChunks(t, src, 200)
	covered := map[int64]bool{}
	for _, ch := range chunks {
		for line := ch.StartLine; line <= ch.EndLine; line++ {
			covered[line] = true
		}
	}
	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		ln := int64(i + 1)
		if !covered[ln] {
			t.Fatalf("uncovered non-empty line %d: %q", ln, line)
		}
	}
}

func TestGroupedDeclBoundaries(t *testing.T) {
	src := []byte("package main\n\nconst (\n\tA = 1\n\tB = 2\n)\n")
	chunks := collectGoChunks(t, src, 80)
	names := map[string]bool{}
	for _, ch := range chunks {
		if ch.SymbolKind == "const" {
			names[ch.SymbolName] = true
		}
	}
	if !names["A"] || !names["B"] {
		t.Fatalf("expected separate const specs, got %+v", toGolden(chunks))
	}
}

func TestCloneCaptures(t *testing.T) {
	if got := cloneCaptures(nil); got != nil {
		t.Fatalf("nil map: got %v want nil", got)
	}
	if got := cloneCaptures(map[string]string{}); got != nil {
		t.Fatalf("empty map: got %v want nil", got)
	}
	src := map[string]string{"scope.receiver": "*Server", "name": "Foo"}
	dst := cloneCaptures(src)
	if dst == nil {
		t.Fatal("expected non-nil copy")
	}
	if dst["scope.receiver"] != "*Server" || dst["name"] != "Foo" {
		t.Fatalf("copy contents: got %+v", dst)
	}
	src["scope.receiver"] = "mutated"
	if dst["scope.receiver"] != "*Server" {
		t.Fatalf("clone must not alias source map: got %q", dst["scope.receiver"])
	}
}

func TestBoundaryCapturesNotAliased(t *testing.T) {
	src := []byte("package main\n\ntype Server struct{}\nfunc (s *Server) Foo() {}\nfunc Bar() {}\n")
	spec := grammars["go"]
	if spec == nil {
		t.Fatal("go grammar not registered")
	}
	parser := spec.pool.borrow()
	defer spec.pool.release(parser)
	tree := parser.Parse(src, nil)
	if tree == nil {
		t.Fatal("parse returned nil tree")
	}
	defer tree.Close()
	root := tree.RootNode()
	boundaries, _, err := indexBoundariesForSpec(spec, root, src, "go", "sample.go")
	if err != nil {
		t.Fatal(err)
	}
	var methodFoo, funcBar BoundaryMeta
	for _, meta := range boundaries {
		switch {
		case meta.Kind == "method" && meta.Name == "Foo":
			methodFoo = meta
		case meta.Kind == "function" && meta.Name == "Bar":
			funcBar = meta
		}
	}
	if methodFoo.Captures == nil || funcBar.Captures == nil {
		t.Fatalf("missing captures: method=%v function=%v", methodFoo.Captures, funcBar.Captures)
	}
	if methodFoo.Captures["scope.receiver"] != "*Server" {
		t.Fatalf("method receiver=%q want *Server", methodFoo.Captures["scope.receiver"])
	}
	if _, ok := funcBar.Captures["scope.receiver"]; ok {
		t.Fatalf("function Bar must not inherit method receiver: %+v", funcBar.Captures)
	}
}

func TestReceiverScopeNormalization(t *testing.T) {
	src := []byte("package main\n\ntype Server struct{}\nfunc (s *Server) Foo() {}\n")
	chunks := collectGoChunks(t, src, 80)
	for _, ch := range chunks {
		if ch.SymbolKind == "method" && ch.SymbolName == "Foo" {
			if strings.Contains(ch.ParentScope, "*Server") {
				t.Fatalf("parent_scope must not contain pointer: %q", ch.ParentScope)
			}
			if !strings.Contains(ch.ParentScope, "type Server") {
				t.Fatalf("expected type Server in scope, got %q", ch.ParentScope)
			}
		}
	}
}

func TestExtendScopeGo(t *testing.T) {
	base := PackageScope("main")
	meta := BoundaryMeta{Kind: "method", Name: "Foo", Captures: map[string]string{"scope.receiver": "*Server"}}
	scope := extendScope(base, meta, "go")
	if scope.String() != "package main > type Server > method Foo" {
		t.Fatalf("scope=%q", scope.String())
	}
	ps := parentScopeForBoundary(base, meta, "go")
	if ps != "package main > type Server" {
		t.Fatalf("parent scope=%q", ps)
	}
}

func TestEmitErrorPropagation(t *testing.T) {
	src := []byte("package main\nfunc F() {}\n")
	want := errors.New("emit failed")
	err := ChunkGo("a.go", src, testCfg(50), token.CharCounter{}, func(*zvec.Chunk) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}

func TestContextPrefixBudget(t *testing.T) {
	counter := token.CharCounter{}
	cfg := Config{ContextPrefix: true, MaxInputTokens: 100, EmbedBudgetRatio: 0.9}
	body := cfg.bodyBudget(counter, "internal/a.go", "package main")
	prefix := contextPrefix("internal/a.go", "package main")
	if body != 90-counter.Count(prefix) {
		t.Fatalf("body budget=%d prefix=%d", body, counter.Count(prefix))
	}
}

func TestNormalizeReceiverType(t *testing.T) {
	if got := normalizeReceiverType("  *http.Server "); got != "http.Server" {
		t.Fatalf("got %q", got)
	}
}

func TestScopeString(t *testing.T) {
	s := PackageScope("auth").WithSegment("type", "Server")
	if s.String() != "package auth > type Server" {
		t.Fatalf("got %q", s.String())
	}
}

func TestParseGoTreeAndBoundaries(t *testing.T) {
	src := []byte("package sample\nfunc F() {}\n")
	tree, cleanup, err := ParseGoTree(src)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	root := tree.RootNode()
	bounds, scope, err := IndexGoBoundaries(root, src)
	if err != nil {
		t.Fatal(err)
	}
	if scope.String() != "package sample" {
		t.Fatalf("scope=%q", scope.String())
	}
	if len(bounds) == 0 {
		t.Fatal("expected boundaries")
	}
	if ParseErrorRate(root) < 0 {
		t.Fatal("negative error rate")
	}
}

func TestChunkGoEmptyTree(t *testing.T) {
	err := ChunkGo("empty.go", []byte(""), testCfg(50), token.CharCounter{}, func(*zvec.Chunk) error { return nil })
	if !errors.Is(err, ErrEmptyTree) {
		t.Fatalf("expected ErrEmptyTree, got %v", err)
	}
}

func TestChunkGoHighParseErrorRate(t *testing.T) {
	// Mostly unparseable — ERROR nodes exceed 30% threshold.
	src := []byte("{{{\n(((\n[[[\n\n")
	err := ChunkGo("bad.go", src, testCfg(50), token.CharCounter{}, func(*zvec.Chunk) error { return nil })
	if !errors.Is(err, ErrHighParseErrorRate) {
		t.Fatalf("expected ErrHighParseErrorRate, got %v", err)
	}
}

func TestParseGoTreeNoLeak(t *testing.T) {
	src := []byte("package main\nfunc F() {}\n")
	for i := 0; i < 1000; i++ {
		tree, cleanup, err := ParseGoTree(src)
		if err != nil {
			t.Fatal(err)
		}
		if tree.RootNode().NamedChildCount() == 0 {
			t.Fatal("empty tree")
		}
		cleanup()
	}
}

func TestChunkGoBoundaryExactBudget(t *testing.T) {
	src := []byte("package main\nfunc F() { return }\n")
	chunks := collectGoChunks(t, src, 35)
	var fn *zvec.Chunk
	for i := range chunks {
		if chunks[i].SymbolName == "F" {
			fn = &chunks[i]
		}
	}
	if fn == nil || fn.ChunkStrategy != "ast" {
		t.Fatalf("expected single function chunk, got %v", toGolden(chunks))
	}
	chunks2 := collectGoChunks(t, src, 15)
	partial := false
	for _, ch := range chunks2 {
		if ch.ChunkStrategy == "partial" || ch.SymbolName == "F" && ch.EndLine < 2 {
			partial = true
		}
	}
	if !partial && len(chunks2) < 2 {
		t.Fatalf("expected split/partial, got %v", toGolden(chunks2))
	}
}

func TestBoundaryKindDefault(t *testing.T) {
	if got := boundaryKindFromCapture("boundary.custom"); got != "custom" {
		t.Fatalf("got %q", got)
	}
	if got := boundaryKindFromCapture("boundary.namespace"); got != "namespace" {
		t.Fatalf("got %q", got)
	}
	if got := boundaryKindFromCapture("other"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestWrapperBoundaryHelpers(t *testing.T) {
	if isWrapperBoundaryNode(nil) || shouldDescendAfterEmit(nil) {
		t.Fatal("nil node must be false")
	}
	src := []byte("export class A { m() {} }\n")
	pool := parserPoolForLang("typescript")
	parser, tree, err := pool.parseTree(src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	defer pool.release(parser)
	var exportNode, classNode *sitter.Node
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "export_statement":
			exportNode = n
		case "class_declaration":
			classNode = n
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(tree.RootNode())
	if exportNode == nil || classNode == nil {
		t.Fatal("expected export and class nodes")
	}
	if !isWrapperBoundaryNode(exportNode) {
		t.Fatal("export_statement must be wrapper boundary")
	}
	if !shouldDescendAfterEmit(exportNode) || !shouldDescendAfterEmit(classNode) {
		t.Fatal("export/class must descend after emit")
	}
	if isWrapperBoundaryNode(classNode) {
		t.Fatal("class_declaration must not be wrapper boundary")
	}
}

func TestWrapperBoundaryOversizePartial(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "testdata", "python", "sample.py"))
	if err != nil {
		t.Fatal(err)
	}
	chunks := collectLangChunks(t, "python", "testdata/python/sample.py", src, 40)
	found := false
	for _, ch := range chunks {
		if ch.SymbolName == "handler" && ch.ChunkStrategy == "partial" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected partial handler chunks, got %+v", toGolden(chunks))
	}
}

func TestModuleLevelAssignmentFilter(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "testdata", "python", "sample.py"))
	if err != nil {
		t.Fatal(err)
	}
	pool := parserPoolForLang("python")
	parser, tree, err := pool.parseTree(src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	defer pool.release(parser)
	bounds, _, err := indexBoundaries(tree.RootNode(), src, "python", "sample.py")
	if err != nil {
		t.Fatal(err)
	}
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if meta, ok := bounds[n.Id()]; ok && meta.Kind == "module_var" {
			if !isModuleLevelNode(n) {
				t.Fatalf("nested module_var boundary at line %d", n.StartPosition().Row+1)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(tree.RootNode())
}

func TestPackageScopeEmpty(t *testing.T) {
	if PackageScope("").String() != "" {
		t.Fatal()
	}
}

func TestExtendScopeTypeConstVar(t *testing.T) {
	base := PackageScope("p")
	cases := []struct {
		kind, name, want string
	}{
		{"type", "T", "package p > type T"},
		{"const", "C", "package p > const C"},
		{"var", "V", "package p > var V"},
	}
	for _, tc := range cases {
		got := extendScope(base, BoundaryMeta{Kind: tc.kind, Name: tc.name}, "go").String()
		if got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.kind, got, tc.want)
		}
	}
}
