//go:build zvec && treesitter

package chunk

import (
	"errors"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func bslHybridOpts(includeSDBL bool) Options {
	return Options{
		ChunkingStrategy: "hybrid",
		MaxInputTokens:   256,
		EmbedBudgetRatio: 1.0,
		MinChunkTokens:   1,
		WindowLines:      5,
		OverlapLines:     1,
		Languages: map[string]config.LanguageConfig{
			"bsl": {Enabled: true, IncludeSDBL: includeSDBL},
		},
	}
}

func productionHybridOpts(includeSDBL bool) Options {
	opts := bslHybridOpts(includeSDBL)
	opts.MinChunkTokens = 10
	return opts
}

func TestRouterDCSExtractsQueryChunks(t *testing.T) {
	r := NewChunkRouter()
	xml := []byte(`<?xml version="1.0"?>
<DataCompositionSchema>
  <query>ВЫБРАТЬ
    Ссылка
  ИЗ
    Справочник.Номенклатура</query>
  <field name="Ref"/>
</DataCompositionSchema>
`)
	var chunks []zvec.Chunk
	err := r.ChunkFile("reports/schema.dcs", xml, bslHybridOpts(true), token.CharCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	queryFound := false
	lineWindowFound := false
	for _, ch := range chunks {
		if ch.ChunkType == "query" && ch.SymbolKind == "query" {
			queryFound = true
			if !strings.Contains(ch.Snippet, "ВЫБРАТЬ") {
				t.Fatalf("query snippet missing SDBL: %+v", ch)
			}
			if ch.StartLine < 2 {
				t.Fatalf("query start_line=%d want >= 2", ch.StartLine)
			}
		}
		if ch.ChunkStrategy == "line_window" {
			lineWindowFound = true
			if strings.Contains(ch.Snippet, "ВЫБРАТЬ") {
				t.Fatalf("line_window chunk duplicated query text: %+v", ch)
			}
		}
	}
	if !queryFound {
		t.Fatalf("missing query chunk: %+v", chunks)
	}
	if !lineWindowFound {
		t.Fatalf("expected line_window chunks for remaining XML: %+v", chunks)
	}
}

func TestRouterDCSEmptyQuerySkipped(t *testing.T) {
	r := NewChunkRouter()
	xml := []byte(`<root><query>   </query><query>NOT A QUERY</query><field/></root>`)
	var chunks []zvec.Chunk
	err := r.ChunkFile("empty.dcs", xml, bslHybridOpts(true), token.CharCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range chunks {
		if ch.ChunkType == "query" {
			t.Fatalf("unexpected query chunk: %+v", ch)
		}
	}
	if len(chunks) == 0 {
		t.Fatal("expected line_window chunks for XML shell")
	}
}

func TestRouterDCSIncludeSDBLDisabled(t *testing.T) {
	r := NewChunkRouter()
	xml := []byte(`<root><query>ВЫБРАТЬ 1</query></root>`)
	var chunks []zvec.Chunk
	err := r.ChunkFile("plain.dcs", xml, bslHybridOpts(false), token.CharCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	for _, ch := range chunks {
		if ch.ChunkType == "query" {
			t.Fatalf("query chunk emitted with include_sdbl=false: %+v", ch)
		}
		if ch.ChunkStrategy != "line_window" {
			t.Fatalf("expected line_window only, got %q", ch.ChunkStrategy)
		}
	}
}

func TestRouterBSLMalformedFallback(t *testing.T) {
	r := NewChunkRouter()
	src := []byte("@@@ @@@ @@@ @@@ @@@\n")
	var chunks []zvec.Chunk
	err := r.ChunkFile("broken/module.bsl", src, bslHybridOpts(true), token.CharCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected line_window fallback chunks")
	}
	for _, ch := range chunks {
		if ch.ChunkStrategy != "line_window" {
			t.Fatalf("expected line_window fallback for malformed BSL, got %q", ch.ChunkStrategy)
		}
	}
}

func TestHybridASTPathDCS(t *testing.T) {
	opts := bslHybridOpts(true)
	if !hybridASTPath("schema.dcs", opts) {
		t.Fatal("expected .dcs to use hybrid AST path when include_sdbl=true")
	}
	optsDisabled := bslHybridOpts(false)
	if hybridASTPath("schema.dcs", optsDisabled) {
		t.Fatal("expected .dcs to skip hybrid AST path when include_sdbl=false")
	}
}

func TestRouterDCSHeuristicCounterProductionDefaults(t *testing.T) {
	r := NewChunkRouter()
	xml := []byte(`<root><query>ВЫБРАТЬ 1</query><field/></root>`)
	var chunks []zvec.Chunk
	err := r.ChunkFile("schema.dcs", xml, productionHybridOpts(true), &token.HeuristicCounter{}, func(ch *zvec.Chunk) error {
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
		if ch.ChunkType == "query" && strings.Contains(ch.Snippet, "ВЫБРАТЬ") {
			queryFound = true
		}
		if ch.ChunkStrategy == "line_window" && strings.Contains(ch.Snippet, "ВЫБРАТЬ") {
			t.Fatalf("small query must not fall through to XML line_window: %+v", ch)
		}
	}
	if !queryFound {
		t.Fatalf("expected query chunk under HeuristicCounter min_chunk_tokens=10: %+v", chunks)
	}
}

func TestRouterDCSQueryErrorFallback(t *testing.T) {
	r := NewChunkRouter()
	xml := []byte(`<root><query>ВЫБРАТЬ 1</query><field/></root>`)
	var chunks []zvec.Chunk
	failAST := true
	err := r.ChunkFile("schema.dcs", xml, productionHybridOpts(true), &token.HeuristicCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil && ch.ChunkType == "query" && ch.ChunkStrategy == "ast" && failAST {
			failAST = false
			return errors.New("emit failed")
		}
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fallbackFound := false
	for _, ch := range chunks {
		if ch.ChunkType == "query" && ch.ChunkStrategy == "line_window" && strings.Contains(ch.Snippet, "ВЫБРАТЬ") {
			fallbackFound = true
		}
		if ch.ChunkStrategy == "line_window" && strings.Contains(ch.Snippet, "ВЫБРАТЬ") && ch.ChunkType != "query" {
			t.Fatalf("query text leaked into XML line_window: %+v", ch)
		}
	}
	if !fallbackFound {
		t.Fatalf("expected line_window fallback for failed query emit: %+v", chunks)
	}
}
