//go:build zvec && treesitter

package chunk_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestProcessBatchesHybridGo(t *testing.T) {
	root := t.TempDir()
	rel := "auth/handler.go"
	if err := os.MkdirAll(filepath.Join(root, "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("package auth\n\nfunc MyFunc() int {\n\treturn 42\n}\n")
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := chunk.Options{
		ChunkingStrategy: "hybrid",
		MaxInputTokens:   256,
		EmbedBudgetRatio: 1.0,
		MinChunkTokens:   1,
		Languages: map[string]config.LanguageConfig{
			"go": {Enabled: true},
		},
	}
	var chunks []zvec.Chunk
	n, err := chunk.ProcessBatches(root, rel, opts, token.CharCounter{}, 8, func(batch []zvec.Chunk) error {
		chunks = append(chunks, batch...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected chunks")
	}
	found := false
	for _, ch := range chunks {
		if ch.SymbolName == "MyFunc" && ch.SymbolKind == "function" && ch.ChunkStrategy == "ast" {
			found = true
		}
	}
	if !found {
		t.Fatalf("chunks=%+v", chunks)
	}
}

func TestProcessBatchesHybridGoAboveStreamThreshold(t *testing.T) {
	root := t.TempDir()
	rel := "big/large.go"
	if err := os.MkdirAll(filepath.Join(root, "big"), 0o755); err != nil {
		t.Fatal(err)
	}
	const targetSize = 300 * 1024 // above default 256 KiB stream threshold
	var b strings.Builder
	b.WriteString("package big\n\n")
	for i := 0; b.Len() < targetSize; i++ {
		fmt.Fprintf(&b, "func Func%d() int {\n\treturn %d\n}\n\n", i, i)
	}
	content := []byte(b.String())
	if len(content) <= 256*1024 {
		t.Fatalf("fixture too small: %d bytes", len(content))
	}
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := chunk.Options{
		ChunkingStrategy:     "hybrid",
		StreamThresholdBytes: 256 * 1024,
		MaxFileBytes:         2 * 1024 * 1024,
		MaxInputTokens:       256,
		EmbedBudgetRatio:     1.0,
		MinChunkTokens:       1,
		Languages: map[string]config.LanguageConfig{
			"go": {Enabled: true},
		},
	}
	var chunks []zvec.Chunk
	n, err := chunk.ProcessBatches(root, rel, opts, token.CharCounter{}, 8, func(batch []zvec.Chunk) error {
		chunks = append(chunks, batch...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected chunks")
	}
	astCount := 0
	lineWindowCount := 0
	for _, ch := range chunks {
		switch ch.ChunkStrategy {
		case "ast":
			astCount++
		case "line_window":
			lineWindowCount++
		}
	}
	if astCount == 0 {
		t.Fatalf("expected AST chunks for large .go file, got ast=%d line_window=%d total=%d sample=%+v",
			astCount, lineWindowCount, n, chunks[0])
	}
	if lineWindowCount == n {
		t.Fatalf("all chunks used line_window; expected AST path above stream threshold")
	}
}

func TestRouterFallbackParseErrorPython(t *testing.T) {
	root := t.TempDir()
	rel := "bad.py"
	content := []byte("def (((:\n    pass\n")
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := chunk.Options{
		ChunkingStrategy: "hybrid",
		MinChunkTokens:   1,
		MaxInputTokens:   50,
		EmbedBudgetRatio: 1.0,
		WindowLines:      5,
		OverlapLines:     1,
		Languages: map[string]config.LanguageConfig{
			"python": {Enabled: true},
		},
	}
	var chunks []zvec.Chunk
	n, err := chunk.ProcessBatches(root, rel, opts, token.CharCounter{}, 4, func(batch []zvec.Chunk) error {
		chunks = append(chunks, batch...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected line_window fallback chunks")
	}
	for _, ch := range chunks {
		if ch.ChunkStrategy != "line_window" {
			t.Fatalf("expected line_window fallback, got strategy=%q", ch.ChunkStrategy)
		}
	}
}

func TestProcessBatchesHybridPython(t *testing.T) {
	root := t.TempDir()
	rel := "sample.py"
	content := []byte("@deco\ndef handler():\n    return 1\n")
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := chunk.Options{
		ChunkingStrategy: "hybrid",
		MaxInputTokens:   256,
		EmbedBudgetRatio: 1.0,
		MinChunkTokens:   1,
		Languages: map[string]config.LanguageConfig{
			"python": {Enabled: true},
		},
	}
	var chunks []zvec.Chunk
	n, err := chunk.ProcessBatches(root, rel, opts, token.CharCounter{}, 8, func(batch []zvec.Chunk) error {
		chunks = append(chunks, batch...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected chunks")
	}
	found := false
	for _, ch := range chunks {
		if ch.SymbolName == "handler" && ch.SymbolKind == "function" && ch.ChunkStrategy == "ast" {
			found = true
		}
	}
	if !found {
		t.Fatalf("chunks=%+v", chunks)
	}
}

func TestRouterFallbackParseError(t *testing.T) {
	root := t.TempDir()
	rel := "bad.go"
	// Same payload as TestChunkGoHighParseErrorRate — exceeds 30% named ERROR nodes.
	content := []byte("{{{\n(((\n[[[\n\n")
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := chunk.Options{
		ChunkingStrategy: "hybrid",
		MinChunkTokens:   1,
		MaxInputTokens:   50,
		EmbedBudgetRatio: 1.0,
		WindowLines:      5,
		OverlapLines:     1,
		Languages: map[string]config.LanguageConfig{
			"go": {Enabled: true},
		},
	}
	var chunks []zvec.Chunk
	n, err := chunk.ProcessBatches(root, rel, opts, token.CharCounter{}, 4, func(batch []zvec.Chunk) error {
		chunks = append(chunks, batch...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected line_window fallback chunks")
	}
	for _, ch := range chunks {
		if ch.ChunkStrategy != "line_window" {
			t.Fatalf("expected line_window fallback, got strategy=%q chunk=%+v", ch.ChunkStrategy, ch)
		}
	}
}
