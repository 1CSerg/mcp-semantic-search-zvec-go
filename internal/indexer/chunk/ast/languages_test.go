//go:build zvec && treesitter

package ast

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

type langFixture struct {
	lang     string
	relPath  string
	subdir   string
	budget   int
	expected []goldenChunk
}

var languageFixtures = []langFixture{
	{
		lang: "python", relPath: "testdata/python/sample.py", subdir: "python", budget: 200,
		expected: []goldenChunk{
			{SymbolName: "handler", SymbolKind: "function", ParentScope: "module sample"},
			{SymbolName: "Inner", SymbolKind: "class", ParentScope: "module sample > function handler"},
			{SymbolName: "DataModel", SymbolKind: "class", ParentScope: "module sample"},
			{SymbolName: "__init__", SymbolKind: "method", ParentScope: "module sample > class DataModel"},
		},
	},
	{
		lang: "javascript", relPath: "testdata/javascript/sample.js", subdir: "javascript", budget: 120,
		expected: []goldenChunk{
			{SymbolName: "arrow", SymbolKind: "function", ParentScope: "module sample"},
			{SymbolName: "DefaultService", SymbolKind: "class", ParentScope: "module sample"},
			{SymbolName: "run", SymbolKind: "method", ParentScope: "class DefaultService"},
		},
	},
	{
		lang: "typescript", relPath: "testdata/typescript/sample.ts", subdir: "typescript", budget: 120,
		expected: []goldenChunk{
			{SymbolName: "User", SymbolKind: "interface", ParentScope: "module sample"},
			{SymbolName: "UserService", SymbolKind: "class", ParentScope: "module sample"},
			{SymbolName: "getUser", SymbolKind: "method", ParentScope: "class UserService"},
			{SymbolName: "AnonymousExport", SymbolKind: "class", ParentScope: "module sample"},
		},
	},
	{
		lang: "tsx", relPath: "testdata/tsx/sample.tsx", subdir: "tsx", budget: 120,
		expected: []goldenChunk{
			{SymbolName: "Greeting", SymbolKind: "function", ParentScope: "module sample"},
			{SymbolName: "App", SymbolKind: "function", ParentScope: "module sample"},
		},
	},
	{
		lang: "tsx", relPath: "testdata/jsx/sample.jsx", subdir: "jsx", budget: 120,
		expected: []goldenChunk{
			{SymbolName: "Widget", SymbolKind: "function", ParentScope: "module sample"},
		},
	},
	{
		lang: "bsl", relPath: "testdata/bsl/sample.bsl", subdir: "bsl", budget: 200,
		expected: []goldenChunk{
			{SymbolName: "РассчитатьСумму", SymbolKind: "procedure", ParentScope: "module sample > region СлужебныеПроцедурыИФункции"},
		},
	},
}

func collectLangChunks(t *testing.T, lang, relPath string, src []byte, budget int) []zvec.Chunk {
	t.Helper()
	var out []zvec.Chunk
	cfg := testCfg(budget)
	if lang == "bsl" {
		cfg.IncludeSDBL = true
	}
	err := ChunkLanguage(lang, relPath, src, cfg, token.CharCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			out = append(out, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ChunkLanguage(%s): %v", lang, err)
	}
	return out
}

func loadLangSample(t *testing.T, subdir string) []byte {
	t.Helper()
	var path string
	switch subdir {
	case "python":
		path = filepath.Join("..", "testdata", subdir, "sample.py")
	case "javascript":
		path = filepath.Join("..", "testdata", subdir, "sample.js")
	case "typescript":
		path = filepath.Join("..", "testdata", subdir, "sample.ts")
	case "tsx":
		path = filepath.Join("..", "testdata", subdir, "sample.tsx")
	case "jsx":
		path = filepath.Join("..", "testdata", subdir, "sample.jsx")
	case "bsl":
		path = filepath.Join("..", "testdata", subdir, "sample.bsl")
	default:
		t.Fatalf("unknown subdir %q", subdir)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestASTChunker_Languages(t *testing.T) {
	for _, fx := range languageFixtures {
		t.Run(fx.lang+"/"+fx.subdir, func(t *testing.T) {
			src := loadLangSample(t, fx.subdir)
			chunks := collectLangChunks(t, fx.lang, fx.relPath, src, fx.budget)
			if len(chunks) == 0 {
				t.Fatal("expected chunks")
			}
			for _, ch := range chunks {
				if ch.ChunkStrategy != "ast" && ch.ChunkStrategy != "partial" {
					t.Fatalf("unexpected strategy %q for %s", ch.ChunkStrategy, fx.lang)
				}
				if ch.ChunkType != "code" && ch.ChunkType != "query" {
					t.Fatalf("unexpected chunk_type %q for %s", ch.ChunkType, fx.lang)
				}
			}
			if len(fx.expected) > 0 {
				assertExpectedSymbols(t, chunks, fx.expected)
			}
		})
	}
}

func assertExpectedSymbols(t *testing.T, chunks []zvec.Chunk, expected []goldenChunk) {
	t.Helper()
	found := make(map[string]goldenChunk)
	for _, ch := range chunks {
		if ch.SymbolName == "" {
			continue
		}
		key := ch.SymbolName + "\x00" + ch.SymbolKind
		found[key] = goldenChunk{
			SymbolName:  ch.SymbolName,
			SymbolKind:  ch.SymbolKind,
			ParentScope: ch.ParentScope,
		}
	}
	for _, want := range expected {
		key := want.SymbolName + "\x00" + want.SymbolKind
		got, ok := found[key]
		if !ok {
			t.Fatalf("missing chunk symbol=%q kind=%q in %+v", want.SymbolName, want.SymbolKind, toGolden(chunks))
		}
		if want.ParentScope != "" && got.ParentScope != want.ParentScope {
			t.Fatalf("symbol=%q parent_scope got %q want %q", want.SymbolName, got.ParentScope, want.ParentScope)
		}
		if want.SymbolKind != "" && got.SymbolKind != want.SymbolKind {
			t.Fatalf("symbol=%q kind got %q want %q", want.SymbolName, got.SymbolKind, want.SymbolKind)
		}
	}
}

func TestChunkLanguageGolden(t *testing.T) {
	for _, fx := range languageFixtures {
		t.Run(fx.lang+"/"+fx.subdir, func(t *testing.T) {
			src := loadLangSample(t, fx.subdir)
			chunks := collectLangChunks(t, fx.lang, fx.relPath, src, fx.budget)
			got := toGolden(chunks)
			data, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			goldenPath := filepath.Join("..", "testdata", fx.subdir, "golden.json")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Log("updated", goldenPath)
				return
			}
			wantData, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden (set UPDATE_GOLDEN=1): %v", err)
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
		})
	}
}

func TestChunkLanguageHighParseErrorRate(t *testing.T) {
	cases := []struct {
		lang string
		ext  string
		src  []byte
	}{
		{lang: "python", ext: "py", src: []byte("def (((:\n    pass\n")},
		{lang: "javascript", ext: "js", src: []byte("const x = {{{\n")},
		{lang: "typescript", ext: "ts", src: []byte("interface {{{\n")},
		{lang: "bsl", ext: "bsl", src: []byte("@@@ @@@ @@@ @@@ @@@\n")},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			rel := "broken/sample." + tc.ext
			err := ChunkLanguage(tc.lang, rel, tc.src, testCfg(50), token.CharCounter{}, func(*zvec.Chunk) error { return nil })
			if !errors.Is(err, ErrHighParseErrorRate) && !errors.Is(err, ErrEmptyTree) {
				t.Fatalf("expected parse fallback error, got %v", err)
			}
		})
	}
}

func TestExtendScopePython(t *testing.T) {
	base := ModuleScope("sample")
	meta := BoundaryMeta{Kind: "function", Name: "handler"}
	scope := extendScope(base, meta, "python")
	if scope.String() != "module sample > function handler" {
		t.Fatalf("scope=%q", scope.String())
	}
	classScope := extendScope(scope, BoundaryMeta{Kind: "class", Name: "Inner"}, "python")
	if classScope.String() != "module sample > function handler > class Inner" {
		t.Fatalf("scope=%q", classScope.String())
	}
}

func TestModuleScopeString(t *testing.T) {
	if ModuleScope("sample").String() != "module sample" {
		t.Fatal()
	}
}

func TestChunkLanguageWrappers(t *testing.T) {
	srcPy := []byte("def f():\n    pass\n")
	srcJS := []byte("function f() {}\n")
	srcTS := []byte("interface I {}\n")
	srcTSX := []byte("const x = 1;\n")
	cfg := testCfg(120)
	emit := func(*zvec.Chunk) error { return nil }
	if err := ChunkPython("a.py", srcPy, cfg, token.CharCounter{}, emit); err != nil {
		t.Fatalf("ChunkPython: %v", err)
	}
	if err := ChunkJavaScript("a.js", srcJS, cfg, token.CharCounter{}, emit); err != nil {
		t.Fatalf("ChunkJavaScript: %v", err)
	}
	if err := ChunkTypeScript("a.ts", srcTS, cfg, token.CharCounter{}, emit); err != nil {
		t.Fatalf("ChunkTypeScript: %v", err)
	}
	if err := ChunkTSX("a.tsx", srcTSX, cfg, token.CharCounter{}, emit); err != nil {
		t.Fatalf("ChunkTSX: %v", err)
	}
	srcBSL := []byte("Процедура Тест()\nКонецПроцедуры\n")
	cfg.IncludeSDBL = true
	if err := ChunkBSL("a.bsl", srcBSL, cfg, token.CharCounter{}, emit); err != nil {
		t.Fatalf("ChunkBSL: %v", err)
	}
}

func TestGoScopeFromRootAndLoadQuery(t *testing.T) {
	src := []byte("package main\nfunc F() {}\n")
	tree, cleanup, err := ParseGoTree(src)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	scope := goScopeFromRoot(tree.RootNode(), src, "")
	if scope.String() != "package main" {
		t.Fatalf("scope=%q", scope.String())
	}
	spec := grammars["typescript"]
	q, err := spec.loadQuery()
	if err != nil || q == nil {
		t.Fatalf("loadQuery: %v", err)
	}
}

func TestEmitPartialWindows(t *testing.T) {
	cfg := testCfg(50)
	cfg.WindowLines = 2
	cfg.OverlapLines = 0
	var count int
	err := emitPartialWindows("a.py", []string{"line1", "line2", "line3"}, 1, cfg, token.CharCounter{}, partialMeta{
		chunkStrategy: "partial",
		symbolKind:    "function",
		symbolName:    "f",
		parentScope:   "module a",
	}, func(*zvec.Chunk) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("expected partial windows")
	}
}
