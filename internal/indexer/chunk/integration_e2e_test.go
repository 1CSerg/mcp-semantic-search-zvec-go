//go:build zvec && treesitter

package chunk_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func minirepoDir() string {
	return filepath.Join("testdata", "integration", "minirepo")
}

func minirepoHybridOpts(contextPrefix bool, maxTokens int) chunk.Options {
	return chunk.Options{
		ChunkingStrategy:  "hybrid",
		MaxInputTokens:    maxTokens,
		EmbedBudgetRatio:  0.9,
		MinChunkTokens:    1,
		ContextPrefix:     contextPrefix,
		ProseOverlapRatio: 0.12,
		WindowLines:       5,
		OverlapLines:      1,
		Languages: map[string]config.LanguageConfig{
			"go":         {Enabled: true},
			"python":     {Enabled: true},
			"javascript": {Enabled: true},
			"typescript": {Enabled: true},
			"bsl":        {Enabled: true, IncludeSDBL: true},
		},
	}
}

func indexMinirepo(t *testing.T, opts chunk.Options) []zvec.Chunk {
	t.Helper()
	root := minirepoDir()
	counter := token.CharCounter{}
	var all []zvec.Chunk
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		_, err = chunk.ProcessBatches(root, rel, opts, counter, 16, func(batch []zvec.Chunk) error {
			all = append(all, batch...)
			return nil
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("expected chunks from minirepo")
	}
	return all
}

func findMatchingChunk(chunks []zvec.Chunk, pathSuffix, symbolName, symbolKind, strategy, query string) *zvec.Chunk {
	var best *zvec.Chunk
	bestScore := -1
	for i := range chunks {
		ch := &chunks[i]
		if pathSuffix != "" && !strings.Contains(ch.RelativePath, pathSuffix) {
			continue
		}
		if symbolName != "" && ch.SymbolName != symbolName && !strings.Contains(ch.Snippet, symbolName) {
			continue
		}
		if symbolKind != "" && ch.SymbolKind != symbolKind {
			continue
		}
		if strategy != "" && ch.ChunkStrategy != strategy {
			continue
		}
		score := keywordScore(query, *ch)
		if symbolName != "" && ch.SymbolName == symbolName {
			score += 10
		}
		if score > bestScore {
			bestScore = score
			best = ch
		}
	}
	return best
}

func keywordScore(query string, ch zvec.Chunk) int {
	q := strings.ToLower(query)
	text := strings.ToLower(ch.Snippet + " " + ch.SymbolName + " " + ch.RelativePath)
	score := 0
	for _, term := range strings.Fields(q) {
		if strings.Contains(text, term) {
			score++
		}
	}
	return score
}

func TestE2E_HybridChunking_Indexing(t *testing.T) {
	chunks := indexMinirepo(t, minirepoHybridOpts(false, 512))
	strategies := map[string]int{}
	for _, ch := range chunks {
		strategies[ch.ChunkStrategy]++
	}
	if strategies["ast"] == 0 {
		t.Fatalf("expected AST chunks, strategies=%v", strategies)
	}
	if strategies["prose"] == 0 {
		t.Fatalf("expected prose chunks, strategies=%v", strategies)
	}
}

type searchExpect struct {
	query      string
	pathSuffix string
	symbolName string
	symbolKind string
	strategy   string
}

func TestE2E_HybridChunking_Search(t *testing.T) {
	chunks := indexMinirepo(t, minirepoHybridOpts(false, 512))
	cases := []searchExpect{
		{query: "auth middleware", pathSuffix: "middleware.go", symbolName: "AuthMiddleware", symbolKind: "function", strategy: "ast"},
		{query: "Процедура Привет", pathSuffix: "Processing.bsl", symbolName: "Привет", symbolKind: "procedure", strategy: "ast"},
		{query: "встроенный запрос остатки", pathSuffix: "Processing.bsl", symbolKind: "query", strategy: "ast"},
		{query: "пакет запросов отчёт", pathSuffix: "Report.dcs", symbolKind: "query", strategy: "ast"},
		{query: "React button component", pathSuffix: "Button.tsx", symbolName: "Button", symbolKind: "function", strategy: "ast"},
		{query: "legacy button", pathSuffix: "LegacyButton.js", strategy: "line_window"},
		{query: "System architecture", pathSuffix: "architecture.md", symbolKind: "section", strategy: "prose"},
		{query: "changelog version table", pathSuffix: "changelog.md", strategy: "prose"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			hit := findMatchingChunk(chunks, tc.pathSuffix, tc.symbolName, tc.symbolKind, tc.strategy, tc.query)
			if hit == nil {
				dumpChunksForDebug(t, chunks)
				t.Fatalf("no chunk for query %q path=%q", tc.query, tc.pathSuffix)
			}
			if tc.pathSuffix != "" && !strings.Contains(hit.RelativePath, tc.pathSuffix) {
				t.Fatalf("path=%q want suffix %q", hit.RelativePath, tc.pathSuffix)
			}
			if tc.symbolName != "" && hit.SymbolName != tc.symbolName && !strings.Contains(hit.Snippet, tc.symbolName) {
				t.Fatalf("symbol_name=%q want %q", hit.SymbolName, tc.symbolName)
			}
			if tc.symbolKind != "" && hit.SymbolKind != tc.symbolKind {
				t.Fatalf("symbol_kind=%q want %q", hit.SymbolKind, tc.symbolKind)
			}
			if tc.strategy != "" && hit.ChunkStrategy != tc.strategy {
				t.Fatalf("chunk_strategy=%q want %q", hit.ChunkStrategy, tc.strategy)
			}
		})
	}
}

func TestE2E_EmbeddedSDBL_InBSL(t *testing.T) {
	chunks := indexMinirepo(t, minirepoHybridOpts(false, 512))
	var queryChunk *zvec.Chunk
	for i := range chunks {
		ch := &chunks[i]
		if !strings.Contains(ch.RelativePath, "Processing.bsl") {
			continue
		}
		if ch.SymbolKind == "query" && strings.Contains(ch.Snippet, "Остатки") {
			queryChunk = ch
			break
		}
	}
	if queryChunk == nil {
		t.Fatal("missing embedded SDBL query chunk in Processing.bsl")
	}
	if queryChunk.ChunkStrategy != "ast" {
		t.Fatalf("chunk_strategy=%q want ast", queryChunk.ChunkStrategy)
	}
}

func TestE2E_SDBL_DCS_File(t *testing.T) {
	chunks := indexMinirepo(t, minirepoHybridOpts(false, 512))
	queryCount := 0
	for _, ch := range chunks {
		if !strings.Contains(ch.RelativePath, "Report.dcs") {
			continue
		}
		if ch.SymbolKind == "query" && ch.ChunkType == "query" {
			queryCount++
			if ch.ChunkStrategy != "ast" {
				t.Fatalf("chunk_strategy=%q want ast", ch.ChunkStrategy)
			}
		}
	}
	if queryCount == 0 {
		t.Fatal("expected query chunks from Report.dcs")
	}
}

func TestE2E_Markdown_TablesFrontMatter(t *testing.T) {
	chunks := indexMinirepo(t, minirepoHybridOpts(false, 512))
	tableFound := false
	for _, ch := range chunks {
		if !strings.Contains(ch.RelativePath, "changelog.md") {
			continue
		}
		if strings.Contains(ch.Snippet, "| Version |") {
			tableFound = true
			if ch.ChunkStrategy != "prose" {
				t.Fatalf("chunk_strategy=%q want prose", ch.ChunkStrategy)
			}
			for _, line := range strings.Split(ch.Snippet, "\n") {
				trim := strings.TrimSpace(line)
				if strings.HasPrefix(trim, "|") && strings.Count(trim, "|") >= 2 && !strings.HasSuffix(trim, "|") {
					t.Fatalf("table row truncated mid-cell: %q", line)
				}
			}
		}
		if strings.Contains(ch.Snippet, "version_schema:") && !strings.Contains(ch.Snippet, "semver") {
			t.Fatalf("front matter line split: %q", ch.Snippet)
		}
	}
	if !tableFound {
		t.Fatal("expected changelog chunk containing version table")
	}
}

func maxEmbedTokens(opts chunk.Options, counter token.TokenCounter) int {
	total := int(float64(opts.MaxInputTokens) * opts.EmbedBudgetRatio)
	if total <= 0 {
		total = opts.MaxInputTokens
	}
	return total
}

func TestE2E_ContextPrefixInBudget(t *testing.T) {
	const profileMaxInput = 8192
	opts := minirepoHybridOpts(true, profileMaxInput)
	counter := token.CharCounter{}
	maxEmbed := maxEmbedTokens(opts, counter)
	root := minirepoDir()
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		_, err = chunk.ProcessBatches(root, rel, opts, counter, 8, func(batch []zvec.Chunk) error {
			for _, ch := range batch {
				embedText := chunk.EmbedTextForChunk(ch.RelativePath, ch.ParentScope, ch.Snippet, opts.ContextPrefix)
				if counter.Count(embedText) > maxEmbed {
					t.Fatalf("%s: embed input %d exceeds profile embed budget %d (max_input_tokens=%d ratio=%v)",
						rel, counter.Count(embedText), maxEmbed, opts.MaxInputTokens, opts.EmbedBudgetRatio)
				}
			}
			return nil
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestE2E_JSXFallback_CorruptJS(t *testing.T) {
	opts := minirepoHybridOpts(false, 512)
	root := minirepoDir()
	rel := "frontend/LegacyButton.js"
	var chunks []zvec.Chunk
	n, err := chunk.ProcessBatches(root, rel, opts, token.CharCounter{}, 8, func(batch []zvec.Chunk) error {
		chunks = append(chunks, batch...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected fallback chunks without crash")
	}
	for _, ch := range chunks {
		if ch.ChunkStrategy != "line_window" {
			t.Fatalf("expected line_window fallback, got %q", ch.ChunkStrategy)
		}
	}
}

func TestMinirepoFixtureManifest(t *testing.T) {
	want := []string{
		"auth/middleware.go",
		"api/handlers.py",
		"frontend/components/Button.tsx",
		"backend/utils.js",
		"frontend/LegacyButton.js",
		"1c/Processing.bsl",
		"1c/reports/Report.dcs",
		"docs/architecture.md",
		"docs/changelog.md",
	}
	for _, rel := range want {
		path := filepath.Join(minirepoDir(), filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing fixture %s: %v", rel, err)
		}
	}
}

func copyMinirepoTo(t *testing.T, destRoot string) {
	t.Helper()
	srcRoot := minirepoDir()
	err := filepath.Walk(srcRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destRoot, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func dumpChunksForDebug(t *testing.T, chunks []zvec.Chunk) {
	t.Helper()
	type row struct {
		Path     string `json:"path"`
		Symbol   string `json:"symbol_name"`
		Kind     string `json:"symbol_kind"`
		Strategy string `json:"chunk_strategy"`
	}
	var rows []row
	for _, ch := range chunks {
		rows = append(rows, row{ch.RelativePath, ch.SymbolName, ch.SymbolKind, ch.ChunkStrategy})
	}
	b, _ := json.MarshalIndent(rows, "", "  ")
	t.Log(string(b))
}
