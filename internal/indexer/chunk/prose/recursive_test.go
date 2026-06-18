package prose

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestConfigBodyBudgetWithPrefix(t *testing.T) {
	cfg := Config{
		MaxInputTokens:   100,
		EmbedBudgetRatio: 0.9,
		ContextPrefix:    true,
	}
	counter := token.CharCounter{}
	budget := cfg.bodyBudget(counter, "doc.md", "section Intro")
	if budget >= 90 {
		t.Fatalf("budget=%d should account for prefix", budget)
	}
}

func TestConfigOverlapTokensDefault(t *testing.T) {
	cfg := Config{ProseOverlapRatio: 0}
	n := cfg.overlapTokens(100)
	if n != 12 {
		t.Fatalf("default overlap=%d want 12", n)
	}
}

func TestIsProsePath(t *testing.T) {
	if !IsProsePath("a.md") || !IsProsePath("b.mdc") || !IsProsePath("c.txt") {
		t.Fatal("expected prose paths")
	}
	if IsProsePath("main.go") {
		t.Fatal("go is not prose")
	}
}

func TestConfigFromOptions(t *testing.T) {
	cfg := ConfigFromOptions(512, 0.8, 0.15, true, 5, 30, 6)
	if cfg.MaxInputTokens != 512 || cfg.ProseOverlapRatio != 0.15 {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestSplitByChars(t *testing.T) {
	counter := token.CharCounter{}
	parts := splitByChars("abcdefghij", 3, counter)
	if len(parts) < 3 {
		t.Fatalf("parts=%v", parts)
	}
}

func TestSplitClauses(t *testing.T) {
	parts := splitClauses("part one; part two: part three")
	if len(parts) < 2 {
		t.Fatalf("parts=%v", parts)
	}
	noSpace := splitClauses("part one;part two")
	if len(noSpace) != 1 {
		t.Fatalf("semicolon without whitespace should not split: parts=%v", noSpace)
	}
}

func TestDeepestSplitLevel(t *testing.T) {
	counter := token.CharCounter{}
	kind := deepestSplitLevel("short", 100, counter)
	if kind != "paragraph" {
		t.Fatalf("kind=%q", kind)
	}
}

func TestTrimToTokenBudget(t *testing.T) {
	counter := token.CharCounter{}
	out := trimToTokenBudget("hello world", 5, counter)
	if counter.Count(out) > 5 {
		t.Fatalf("count=%d out=%q", counter.Count(out), out)
	}
}

func TestParseHeadingAndFence(t *testing.T) {
	if _, _, ok := parseHeading("## Title"); !ok {
		t.Fatal("heading")
	}
	if _, ok := parseFence("```go"); !ok {
		t.Fatal("fence")
	}
	if !isFenceClose("```", '`', 3) {
		t.Fatal("exact fence line should close")
	}
	if isFenceClose("```go", '`', 3) {
		t.Fatal("fence with info string must not close")
	}
	if !isTableRow("| a | b |") {
		t.Fatal("table row")
	}
}

func TestUpdateHeadingStack(t *testing.T) {
	stack := updateHeadingStack(nil, 1, "A")
	stack = updateHeadingStack(stack, 2, "B")
	stack = updateHeadingStack(stack, 1, "C")
	if len(stack) != 1 || stack[0] != "C" {
		t.Fatalf("stack=%v", stack)
	}
}

func TestEmitPartialWindows(t *testing.T) {
	cfg := testCfg(20)
	lines := []string{"a", "b", "c", "d", "e", "f", "g"}
	var n int
	err := emitPartialWindows("x.md", lines, 1, cfg, token.CharCounter{}, partialMeta{
		chunkStrategy: "partial",
		symbolKind:    "code_block",
	}, func(ch *zvec.Chunk) error {
		if ch != nil {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected partial chunks")
	}
}

func TestChunkPlainTextOnly(t *testing.T) {
	chunks := collectChunks(t, "note.txt", []byte("one two three"), testCfg(50))
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
}

func TestNormalizeContent(t *testing.T) {
	out := normalizeContent([]byte{0xEF, 0xBB, 0xBF, 'a', '\r', '\n', 'b'})
	if string(out) != "a\nb" {
		t.Fatalf("got %q", out)
	}
}

func TestContextPrefixBudgetIntegration(t *testing.T) {
	cfg := Config{
		MaxInputTokens:    80,
		EmbedBudgetRatio:  1.0,
		ContextPrefix:     true,
		ProseOverlapRatio: 0.1,
	}
	content := "# Scope Test\n\nBody paragraph with enough text.\n"
	var chunks []zvec.Chunk
	err := ChunkMarkdown("doc.md", content, cfg, token.CharCounter{}, func(ch *zvec.Chunk) error {
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
}

func TestRecursiveNilCounter(t *testing.T) {
	parts := RecursiveSplit("hello", 10, nil)
	if len(parts) != 1 || parts[0].Text != "hello" {
		t.Fatalf("parts=%v", parts)
	}
}

func TestSplitParagraphsSingle(t *testing.T) {
	parts := splitParagraphs("no double newline")
	if len(parts) != 1 {
		t.Fatalf("parts=%v", parts)
	}
}

func TestHeadingHasNoOverlap(t *testing.T) {
	if !headingHasNoOverlap("# Title") {
		t.Fatal("expected heading")
	}
}
