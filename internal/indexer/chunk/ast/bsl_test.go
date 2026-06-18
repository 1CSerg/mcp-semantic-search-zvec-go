//go:build zvec && treesitter

package ast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_bsl "github.com/tree-sitter/tree-sitter-bsl/bindings/go"
)

func TestBSLLoadGrammar(t *testing.T) {
	lang := sitter.NewLanguage(tree_sitter_bsl.Language())
	if lang == nil {
		t.Fatal("nil language")
	}
}

func TestBSLSampleParse(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"minimal", "Процедура Тест()\nКонецПроцедуры\n"},
		{"sample_file", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.src)
			if tc.name == "sample_file" {
				data, err := os.ReadFile(filepath.Join("..", "testdata", "bsl", "sample.bsl"))
				if err != nil {
					t.Fatal(err)
				}
				src = data
			}
			parser := sitter.NewParser()
			defer parser.Close()
			lang := bslLang()
			if err := parser.SetLanguage(lang); err != nil {
				t.Fatalf("SetLanguage: %v", err)
			}
			tree := parser.Parse(src, nil)
			if tree == nil {
				t.Fatal("parse returned nil tree")
			}
			defer tree.Close()
			root := tree.RootNode()
			if root == nil {
				t.Fatal("nil root")
			}
			t.Logf("has_error=%v children=%d sexp=%s", root.HasError(), root.NamedChildCount(), root.ToSexp())
		})
	}
}

func TestChunkBSLFunctionDefinition(t *testing.T) {
	src := []byte(`Функция ПолучитьЗначение() Экспорт
    Возврат 1;
КонецФункции
`)
	var chunks []zvec.Chunk
	cfg := Config{
		MinChunkTokens:   10,
		MaxInputTokens:   256,
		EmbedBudgetRatio: 1.0,
		IncludeSDBL:      true,
	}
	err := ChunkBSL("module.bsl", src, cfg, &token.HeuristicCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var fn *zvec.Chunk
	for i := range chunks {
		if chunks[i].SymbolKind == "function" && chunks[i].SymbolName == "ПолучитьЗначение" {
			fn = &chunks[i]
			break
		}
	}
	if fn == nil {
		t.Fatalf("missing function chunk: %+v", chunks)
	}
	if fn.StartLine != 1 || fn.EndLine != 3 {
		t.Fatalf("function lines got %d-%d want 1-3", fn.StartLine, fn.EndLine)
	}
	if fn.ChunkStrategy != "ast" {
		t.Fatalf("chunk_strategy=%q want ast", fn.ChunkStrategy)
	}
	if !strings.Contains(fn.Snippet, "КонецФункции") {
		t.Fatalf("snippet missing КонецФункции: %q", fn.Snippet)
	}
}

func TestChunkBSLEmbeddedQueryHeuristicCounter(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "testdata", "bsl", "sample.bsl"))
	if err != nil {
		t.Fatal(err)
	}
	var chunks []zvec.Chunk
	cfg := Config{
		MinChunkTokens:   10,
		MaxInputTokens:   256,
		EmbedBudgetRatio: 1.0,
		IncludeSDBL:      true,
	}
	err = ChunkBSL("testdata/bsl/sample.bsl", src, cfg, &token.HeuristicCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	queryFound := false
	for _, ch := range chunks {
		if ch.ChunkType == "query" && ch.SymbolKind == "query" && strings.Contains(ch.Snippet, "ВЫБРАТЬ") {
			queryFound = true
		}
	}
	if !queryFound {
		t.Fatalf("embedded query missing under HeuristicCounter: %+v", chunks)
	}
}
