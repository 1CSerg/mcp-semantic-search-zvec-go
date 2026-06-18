package prose

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func testCfg(budget int) Config {
	return Config{
		MaxInputTokens:    budget,
		EmbedBudgetRatio:  1.0,
		ProseOverlapRatio: 0.12,
		MinChunkTokens:    1,
		WindowLines:       5,
		OverlapLines:      1,
	}
}

func collectChunks(t *testing.T, rel string, data []byte, cfg Config) []zvec.Chunk {
	t.Helper()
	var out []zvec.Chunk
	err := ChunkFile(rel, data, cfg, token.CharCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			out = append(out, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type goldenChunk struct {
	StartLine     int64  `json:"start_line"`
	EndLine       int64  `json:"end_line"`
	SymbolName    string `json:"symbol_name"`
	SymbolKind    string `json:"symbol_kind"`
	ChunkStrategy string `json:"chunk_strategy"`
	ChunkType     string `json:"chunk_type"`
	TokenCount    int    `json:"token_count,omitempty"`
}

func chunksToGolden(chunks []zvec.Chunk, counter token.TokenCounter) []goldenChunk {
	out := make([]goldenChunk, 0, len(chunks))
	for _, ch := range chunks {
		out = append(out, goldenChunk{
			StartLine:     ch.StartLine,
			EndLine:       ch.EndLine,
			SymbolName:    ch.SymbolName,
			SymbolKind:    ch.SymbolKind,
			ChunkStrategy: ch.ChunkStrategy,
			ChunkType:     ch.ChunkType,
			TokenCount:    counter.Count(ch.Snippet),
		})
	}
	return out
}

func loadFixture(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join("..", "testdata", "prose", name))
}

func loadGolden(name string) ([]goldenChunk, error) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "prose", name))
	if err != nil {
		return nil, err
	}
	var out []goldenChunk
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

var goldenFixtures = []struct {
	file   string
	golden string
	rel    string
	budget int
}{
	{"simple.txt", "simple_expected.json", "docs/simple.txt", 120},
	{"markdown_basic.md", "markdown_basic_expected.json", "README.md", 120},
	{"markdown_code.md", "markdown_code_expected.json", "docs/code.md", 80},
	{"sentences.txt", "sentences_expected.json", "notes/sentences.txt", 100},
	{"lists_tables.md", "lists_tables_expected.json", "docs/lists.md", 120},
	{"rule.mdc", "rule_expected.json", ".cursor/rules/rule.mdc", 120},
}

func TestGoldenFixtures(t *testing.T) {
	counter := token.CharCounter{}
	for _, fx := range goldenFixtures {
		t.Run(fx.file, func(t *testing.T) {
			data, err := loadFixture(fx.file)
			if err != nil {
				t.Fatal(err)
			}
			cfg := testCfg(fx.budget)
			chunks := collectChunks(t, fx.rel, data, cfg)
			if len(chunks) == 0 {
				t.Fatal("expected chunks")
			}
			for _, ch := range chunks {
				if ch.ChunkType != "markdown" {
					t.Fatalf("chunk_type=%q want markdown", ch.ChunkType)
				}
				bodyBudget := cfg.bodyBudget(counter, fx.rel, ch.ParentScope)
				if counter.Count(ch.Snippet) > bodyBudget+cfg.overlapTokens(bodyBudget)+5 {
					t.Fatalf("chunk exceeds budget: count=%d budget=%d snippet=%q", counter.Count(ch.Snippet), bodyBudget, ch.Snippet)
				}
			}
			expected, err := loadGolden(fx.golden)
			if err != nil {
				t.Fatalf("load golden: %v", err)
			}
			got := chunksToGolden(chunks, counter)
			gotJSON, _ := json.MarshalIndent(got, "", "  ")
			expJSON, _ := json.MarshalIndent(expected, "", "  ")
			if string(gotJSON) != string(expJSON) {
				t.Fatalf("golden mismatch\n got:\n%s\n want:\n%s", gotJSON, expJSON)
			}
		})
	}
}

func TestOverlapWordBoundary(t *testing.T) {
	counter := token.CharCounter{}
	prev := "alpha beta gamma delta epsilon"
	ov := overlapSuffix(prev, 10, counter)
	if ov == "" {
		t.Fatal("expected overlap")
	}
	if !overlapStartsAtBoundary(prev, ov) {
		t.Fatalf("overlap not at word/sentence boundary: %q in %q", ov, prev)
	}
	if strings.Contains(ov, "lph") || strings.Contains(ov, "pha") && !strings.HasPrefix(strings.TrimSpace(ov), "alpha") {
		t.Fatalf("overlap split mid-word: %q", ov)
	}
	prevRU := "первое второе третье четвертое"
	ovRU := overlapSuffix(prevRU, 15, counter)
	if ovRU == "" {
		t.Fatal("expected cyrillic overlap")
	}
	if !overlapStartsAtBoundary(prevRU, ovRU) {
		t.Fatalf("cyrillic overlap not at boundary: %q in %q", ovRU, prevRU)
	}
	if strings.Contains(ovRU, "орое") || strings.Contains(ovRU, "реть") {
		t.Fatalf("overlap split cyrillic mid-word: %q", ovRU)
	}
}

func TestNoOverlapOnHeadingBoundary(t *testing.T) {
	content := "# First\n\nBody one.\n\n# Second\n\nBody two.\n"
	cfg := testCfg(200)
	chunks := collectChunks(t, "doc.md", []byte(content), cfg)
	if len(chunks) < 2 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	second := chunks[1]
	if strings.Contains(second.Snippet, "Body one") {
		t.Fatalf("second chunk must not contain previous section body: %q", second.Snippet)
	}
	if !strings.Contains(second.Snippet, "# Second") && !strings.HasPrefix(strings.TrimSpace(second.Snippet), "#") {
		// second chunk should start with heading or its content without prior overlap
		if strings.Contains(second.Snippet, "one") {
			t.Fatalf("overlap leaked across heading: %q", second.Snippet)
		}
	}
}

func TestRecursiveSplitRespectsBudget(t *testing.T) {
	counter := token.CharCounter{}
	text := strings.Repeat("word ", 50)
	budget := 20
	parts := RecursiveSplit(text, budget, counter)
	for _, p := range parts {
		if counter.Count(p.Text) > budget {
			t.Fatalf("piece exceeds budget: count=%d budget=%d part=%q", counter.Count(p.Text), budget, p.Text)
		}
	}
}

func TestChunkTypeMarkdownForMDC(t *testing.T) {
	data, err := loadFixture("rule.mdc")
	if err != nil {
		t.Fatal(err)
	}
	chunks := collectChunks(t, "rule.mdc", data, testCfg(120))
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	for _, ch := range chunks {
		if ch.ChunkType != "markdown" {
			t.Fatalf("chunk_type=%q want markdown", ch.ChunkType)
		}
	}
}

func TestCodeBlockPartialFallback(t *testing.T) {
	data, err := loadFixture("markdown_code.md")
	if err != nil {
		t.Fatal(err)
	}
	chunks := collectChunks(t, "code.md", data, testCfg(80))
	foundPartial := false
	for _, ch := range chunks {
		if ch.SymbolKind == "code_block" && ch.ChunkStrategy == "partial" {
			foundPartial = true
		}
	}
	if !foundPartial {
		t.Fatal("expected partial code_block chunk for oversized fence")
	}
}

func TestPropertyBudgetOnFixtures(t *testing.T) {
	counter := token.CharCounter{}
	for _, fx := range goldenFixtures {
		t.Run(fx.file, func(t *testing.T) {
			data, err := loadFixture(fx.file)
			if err != nil {
				t.Fatal(err)
			}
			cfg := testCfg(fx.budget)
			chunks := collectChunks(t, fx.rel, data, cfg)
			for i, ch := range chunks {
				max := cfg.MaxInputTokens
				if counter.Count(ch.Snippet) > max {
					t.Fatalf("chunk %d count=%d max=%d", i, counter.Count(ch.Snippet), max)
				}
			}
		})
	}
}

func TestFrontMatterSectionKind(t *testing.T) {
	content := "---\nkey: value\n---\n\n# Title\n\nBody.\n"
	segs := parseMarkdownSegments(content)
	if segs[0].kind != segFrontMatter {
		t.Fatalf("kind=%v", segs[0].kind)
	}
	if segs[0].symbolKind != "section" {
		t.Fatalf("segment symbolKind=%q", segs[0].symbolKind)
	}
	chunks := collectChunks(t, "fm.md", []byte(content), testCfg(120))
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	if chunks[0].SymbolKind != "section" {
		t.Fatalf("chunk symbolKind=%q snippet=%q", chunks[0].SymbolKind, chunks[0].Snippet)
	}
}

func TestParseMarkdownSegments(t *testing.T) {
	segs := parseMarkdownSegments("---\na: b\n---\n\n# H\n\nText\n")
	if len(segs) < 2 {
		t.Fatalf("segments=%d", len(segs))
	}
	if segs[0].kind != segFrontMatter {
		t.Fatalf("first kind=%v", segs[0].kind)
	}
}

func TestHeadingWithListPreservesSection(t *testing.T) {
	content := "# Section Title\n- item one\n- item two\n\nPlain paragraph.\n"
	chunks := collectChunks(t, "lists.md", []byte(content), testCfg(200))
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	first := chunks[0]
	if first.SymbolName != "Section Title" || first.SymbolKind != "section" {
		t.Fatalf("first chunk metadata: name=%q kind=%q", first.SymbolName, first.SymbolKind)
	}
	for _, ch := range chunks[1:] {
		if ch.SymbolKind == "section" && ch.SymbolName == "Section Title" && !strings.Contains(ch.Snippet, "# Section Title") {
			t.Fatalf("section metadata leaked to non-heading chunk: %q", ch.Snippet)
		}
	}
}

func TestMergeGreedy(t *testing.T) {
	counter := token.CharCounter{}
	parts := []string{"aa", "bb", "cc"}
	merged := mergeGreedy(parts, " ", 10, counter)
	if len(merged) != 1 {
		t.Fatalf("merged=%v", merged)
	}
}

func TestMergedParagraphLineNumbersTripleNewline(t *testing.T) {
	counter := token.CharCounter{}
	text := "Line A\n\n\nLine B"
	budget := counter.Count("Line A") + 2 + counter.Count("Line B")
	pieces := RecursiveSplit(text, budget, counter)
	if len(pieces) != 1 {
		t.Fatalf("expected single merged piece, got %d: %+v", len(pieces), pieces)
	}
	if pieces[0].Text != "Line A\n\nLine B" {
		t.Fatalf("merged text=%q", pieces[0].Text)
	}
	if pieces[0].Start != 0 || pieces[0].End != len(text) {
		t.Fatalf("span=%d-%d want 0-%d (must cover triple newline gap)", pieces[0].Start, pieces[0].End, len(text))
	}
	chunks := collectChunks(t, "triple.txt", []byte(text), testCfg(budget))
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d want 1", len(chunks))
	}
	if chunks[0].StartLine != 1 || chunks[0].EndLine != 4 {
		t.Fatalf("lines=%d-%d want 1-4", chunks[0].StartLine, chunks[0].EndLine)
	}
}

func TestSplitSentences(t *testing.T) {
	parts := splitSentences("Hello world. Next sentence! And third?")
	if len(parts) < 2 {
		t.Fatalf("parts=%v", parts)
	}
}
