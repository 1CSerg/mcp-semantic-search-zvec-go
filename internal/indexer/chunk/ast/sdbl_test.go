package ast

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestStripBSLQueryString(t *testing.T) {
	raw := "\"ВЫБРАТЬ\n        |   Сумма\n        |ИЗ\n        |   Документ.Продажа\""
	got := StripBSLQueryString(raw)
	want := "ВЫБРАТЬ\nСумма\nИЗ\nДокумент.Продажа"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSplitSDBLQueries(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "testdata", "sdbl", "sample.sdbl"))
	if err != nil {
		t.Fatal(err)
	}
	queries := splitSDBLQueries(string(src))
	if len(queries) != 2 {
		t.Fatalf("queries=%d", len(queries))
	}
}

func TestChunkSDBLText(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "testdata", "sdbl", "sample.sdbl"))
	if err != nil {
		t.Fatal(err)
	}
	var chunks []zvec.Chunk
	cfg := Config{MinChunkTokens: 1, MaxInputTokens: 200, EmbedBudgetRatio: 1.0}
	err = ChunkSDBLText("testdata/sdbl/sample.sdbl", string(src), 1, cfg, token.CharCounter{}, "module sample", func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected sdbl chunks")
	}
	queryCount := 0
	for _, ch := range chunks {
		if ch.ChunkType != "query" {
			t.Fatalf("chunk_type=%q", ch.ChunkType)
		}
		if ch.SymbolKind == "query" || ch.SymbolKind == "query_package" {
			queryCount++
		}
	}
	if queryCount == 0 {
		t.Fatalf("chunks=%+v", chunks)
	}
}

func TestExtractDCSQueries(t *testing.T) {
	xml := []byte(`<root><query>ВЫБРАТЬ 1</query><other/></root>`)
	got := ExtractDCSQueries(xml)
	if len(got) != 1 || got[0] != "ВЫБРАТЬ 1" {
		t.Fatalf("got=%v", got)
	}
}

func TestExtractDCSQueriesWithLines(t *testing.T) {
	xml := []byte("<root>\n<query>\nВЫБРАТЬ 1\n</query>\n</root>")
	got := ExtractDCSQueriesWithLines(xml)
	if len(got) != 1 {
		t.Fatalf("queries=%d", len(got))
	}
	if got[0].Text != "ВЫБРАТЬ 1" {
		t.Fatalf("text=%q", got[0].Text)
	}
	if got[0].StartLine != 2 {
		t.Fatalf("start_line=%d want 2", got[0].StartLine)
	}
}

func TestStripDCSQueryBlocks(t *testing.T) {
	xml := []byte(`<root><query>ВЫБРАТЬ 1</query><meta/></root>`)
	got := string(StripDCSQueryBlocks(xml))
	if strings.Contains(got, "ВЫБРАТЬ") {
		t.Fatalf("query text not stripped: %q", got)
	}
	if !strings.Contains(got, "<meta/>") {
		t.Fatalf("xml metadata removed: %q", got)
	}
}

func TestChunkSDBLText_SplitsOversizedPackage(t *testing.T) {
	src := "ВЫБРАТЬ A ИЗ T1;\nВЫБРАТЬ B ИЗ T2;\n"
	var chunks []zvec.Chunk
	cfg := Config{MinChunkTokens: 1, MaxInputTokens: 30, EmbedBudgetRatio: 1.0}
	err := ChunkSDBLText("testdata/sdbl/split.sdbl", src, 1, cfg, token.CharCounter{}, "module sample", func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks=%d want 2: %+v", len(chunks), chunks)
	}
	for _, ch := range chunks {
		if ch.SymbolKind != "query" {
			t.Fatalf("symbol_kind=%q want query", ch.SymbolKind)
		}
		if ch.ChunkStrategy != "ast" {
			t.Fatalf("chunk_strategy=%q want ast", ch.ChunkStrategy)
		}
	}
}

func TestChunkSDBLTextGolden(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "testdata", "sdbl", "sample.sdbl"))
	if err != nil {
		t.Fatal(err)
	}
	var chunks []zvec.Chunk
	cfg := Config{MinChunkTokens: 1, MaxInputTokens: 200, EmbedBudgetRatio: 1.0}
	err = ChunkSDBLText("testdata/sdbl/sample.sdbl", string(src), 1, cfg, token.CharCounter{}, "module sample", func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := sdblToGolden(chunks)
	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("..", "testdata", "sdbl", "golden.json")
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
	var want []sdblGoldenChunk
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

type sdblGoldenChunk struct {
	StartLine     int64  `json:"start_line"`
	EndLine       int64  `json:"end_line"`
	SymbolName    string `json:"symbol_name"`
	SymbolKind    string `json:"symbol_kind"`
	ChunkStrategy string `json:"chunk_strategy"`
	ParentScope   string `json:"parent_scope"`
}

func sdblToGolden(chunks []zvec.Chunk) []sdblGoldenChunk {
	out := make([]sdblGoldenChunk, 0, len(chunks))
	for _, ch := range chunks {
		out = append(out, sdblGoldenChunk{
			StartLine:     ch.StartLine,
			EndLine:       ch.EndLine,
			SymbolName:    ch.SymbolName,
			SymbolKind:    ch.SymbolKind,
			ChunkStrategy: ch.ChunkStrategy,
			ParentScope:   ch.ParentScope,
		})
	}
	return out
}

func productionSDBLConfig() Config {
	return Config{
		MinChunkTokens:   10,
		MaxInputTokens:   256,
		EmbedBudgetRatio: 1.0,
		WindowLines:      5,
		OverlapLines:     1,
	}
}

func TestChunkSDBLTextHeuristicCounterProductionDefaults(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "testdata", "sdbl", "sample.sdbl"))
	if err != nil {
		t.Fatal(err)
	}
	var chunks []zvec.Chunk
	cfg := productionSDBLConfig()
	err = ChunkSDBLText("testdata/sdbl/sample.sdbl", string(src), 1, cfg, &token.HeuristicCounter{}, "module sample", func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected sdbl query chunks under production token policy")
	}
	for _, ch := range chunks {
		if ch.ChunkType != "query" {
			t.Fatalf("chunk_type=%q", ch.ChunkType)
		}
	}
}

func TestChunkSDBLTextSmallQueryEmits(t *testing.T) {
	cfg := productionSDBLConfig()
	var chunks []zvec.Chunk
	err := ChunkSDBLText("q.sdbl", "ВЫБРАТЬ 1", 1, cfg, &token.HeuristicCounter{}, "module q", func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d want 1: %+v", len(chunks), chunks)
	}
	if chunks[0].SymbolKind != "query" {
		t.Fatalf("symbol_kind=%q want query", chunks[0].SymbolKind)
	}
}
