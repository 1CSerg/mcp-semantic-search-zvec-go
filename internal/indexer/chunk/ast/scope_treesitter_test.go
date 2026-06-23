//go:build zvec && treesitter

package ast

import (
	"strings"
	"testing"
)

func TestExtendScopePackage(t *testing.T) {
	base := PackageScope("main")
	got := extendScope(base, BoundaryMeta{Kind: "function", Name: "Auth"}, "go")
	want := "package main > func Auth"
	if got.String() != want {
		t.Fatalf("got %q want %q", got.String(), want)
	}
}

func TestBoundaryKindFromCapture(t *testing.T) {
	if boundaryKindFromCapture("boundary.method") != "method" {
		t.Fatal()
	}
	if boundaryKindFromCapture("boundary.assignment") != "module_var" {
		t.Fatal()
	}
	if boundaryKindFromCapture("boundary.interface") != "interface" {
		t.Fatal()
	}
}

func TestClassScopeOnly(t *testing.T) {
	if classScopeOnly(ModuleScope("m")) != "module m" {
		t.Fatal()
	}
}

func TestExtendScopeClass(t *testing.T) {
	got := extendScope(ModuleScope("s"), BoundaryMeta{Kind: "class", Name: "C"}, "python").String()
	if got != "module s > class C" {
		t.Fatalf("got %q", got)
	}
}

func TestExtendScopeEmptyKind(t *testing.T) {
	base := PackageScope("main")
	if got := extendScope(base, BoundaryMeta{}, "go"); got.String() != base.String() {
		t.Fatalf("got %q want %q", got.String(), base.String())
	}
}

func TestExtendScopeProcedureImportDefault(t *testing.T) {
	base := ModuleScope("m")
	cases := []struct {
		kind, name, want string
	}{
		{"procedure", "P", "module m > procedure P"},
		{"import", "fmt", "module m > import fmt"},
		{"region", "R", "module m > region R"},
	}
	for _, tc := range cases {
		got := extendScope(base, BoundaryMeta{Kind: tc.kind, Name: tc.name}, "bsl").String()
		if got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.kind, got, tc.want)
		}
	}
}

func TestParentScopeForBoundaryMethodWithoutReceiver(t *testing.T) {
	scope := PackageScope("main")
	ps := parentScopeForBoundary(scope, BoundaryMeta{Kind: "method", Name: "M"}, "go")
	if ps != "package main" {
		t.Fatalf("go: got %q", ps)
	}
	ps = parentScopeForBoundary(ModuleScope("sample"), BoundaryMeta{Kind: "method", Name: "m"}, "javascript")
	if ps != "module sample" {
		t.Fatalf("javascript: got %q", ps)
	}
}

func TestFirstCaptureName(t *testing.T) {
	if got := firstCaptureName(map[string]string{"name": "foo"}); got != "foo" {
		t.Fatalf("got %q", got)
	}
	if got := firstCaptureName(map[string]string{"other": "bar"}); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestBoundaryKindFromCaptureKinds(t *testing.T) {
	for in, want := range map[string]string{
		"boundary.function":      "function",
		"boundary.region":        "region",
		"boundary.query":         "query",
		"boundary.query_package": "query_package",
		"boundary.procedure":     "procedure",
		"boundary.enum":          "enum",
		"boundary.type_alias":    "type_alias",
		"boundary.var":           "var",
		"boundary.const":         "const",
		"boundary.expression":    "module_var",
		"boundary.type":          "type",
	} {
		if got := boundaryKindFromCapture(in); got != want {
			t.Fatalf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestIndexBoundariesUnknownLanguage(t *testing.T) {
	_, _, err := indexBoundaries(nil, nil, "unknown", "")
	if err == nil || !strings.Contains(err.Error(), "unknown language") {
		t.Fatalf("err=%v", err)
	}
}

func TestIsPythonModuleVarBoundary(t *testing.T) {
	if !isPythonModuleVarBoundary("boundary.assignment") || !isPythonModuleVarBoundary("boundary.expression") {
		t.Fatal("expected module var captures")
	}
	if isPythonModuleVarBoundary("boundary.function") {
		t.Fatal("function is not module var boundary")
	}
}

func TestIsModuleLevelNodeNil(t *testing.T) {
	if isModuleLevelNode(nil) {
		t.Fatal("nil node is not module level")
	}
}

func TestNodeContainsAndDirectChildNil(t *testing.T) {
	if nodeContains(nil, nil) || isDirectChild(nil, nil) {
		t.Fatal("nil nodes must be false")
	}
}

func TestIsExportInnerDeclaration(t *testing.T) {
	if !isExportInnerDeclaration("function_declaration") {
		t.Fatal("expected export inner declaration")
	}
	if isExportInnerDeclaration("expression_statement") {
		t.Fatal("unexpected export inner declaration")
	}
}

func TestFilterNestedBoundaryEntriesPassthrough(t *testing.T) {
	if got := filterNestedBoundaryEntries(nil); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	one := []boundaryEntry{{meta: BoundaryMeta{Kind: "function", Name: "f"}}}
	if len(filterNestedBoundaryEntries(one)) != 1 {
		t.Fatal("single entry must pass through")
	}
}

func TestLoadQueryCached(t *testing.T) {
	spec := grammars["python"]
	q1, err1 := spec.loadQuery()
	q2, err2 := spec.loadQuery()
	if err1 != nil || err2 != nil {
		t.Fatalf("loadQuery: %v %v", err1, err2)
	}
	if q1 == nil || q1 != q2 {
		t.Fatal("expected cached query pointer")
	}
}

func TestIndexBoundariesModuleScopeFallback(t *testing.T) {
	src := []byte("const x = 1;\n")
	pool := parserPoolForLang("javascript")
	parser, tree, err := pool.parseTree(src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	defer pool.release(parser)
	_, scope, err := indexBoundaries(tree.RootNode(), src, "javascript", "dir/sample.js")
	if err != nil {
		t.Fatal(err)
	}
	if scope.String() != "module sample" {
		t.Fatalf("scope=%q", scope.String())
	}
}

func TestFilterNestedDecoratedDefinition(t *testing.T) {
	src := []byte("@deco\ndef foo():\n    pass\n")
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
	hasFoo := false
	for _, meta := range bounds {
		if meta.Name == "foo" && meta.Kind == "function" {
			hasFoo = true
		}
	}
	if !hasFoo {
		t.Fatalf("expected foo boundary, got %+v", bounds)
	}
}
