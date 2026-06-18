//go:build zvec && treesitter

package chunk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func hybridLangOpts(lang string) Options {
	return Options{
		ChunkingStrategy: "hybrid",
		MaxInputTokens:   256,
		EmbedBudgetRatio: 1.0,
		MinChunkTokens:   1,
		Languages: map[string]config.LanguageConfig{
			lang: {Enabled: true},
		},
	}
}

func TestRouterJSXUsesTSXParser(t *testing.T) {
	root := t.TempDir()
	rel := "ui/widget.jsx"
	if err := os.MkdirAll(filepath.Join(root, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("export function Widget() { return <span>ok</span>; }\n")
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := hybridLangOpts("typescript")
	var chunks []zvec.Chunk
	n, err := ProcessBatches(root, rel, opts, token.CharCounter{}, 8, func(batch []zvec.Chunk) error {
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
		if ch.ChunkStrategy == "line_window" {
			t.Fatalf("jsx must not fall back to line_window: %+v", ch)
		}
		if ch.SymbolName == "Widget" && ch.SymbolKind == "function" {
			found = true
		}
	}
	if !found {
		t.Fatalf("chunks=%+v", chunks)
	}
}

func TestRouterJSWithJSXUsesAST(t *testing.T) {
	root := t.TempDir()
	rel := "ui/app.js"
	if err := os.MkdirAll(filepath.Join(root, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("export function App() { return <div>hi</div>; }\n")
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := hybridLangOpts("javascript")
	var chunks []zvec.Chunk
	n, err := ProcessBatches(root, rel, opts, token.CharCounter{}, 8, func(batch []zvec.Chunk) error {
		chunks = append(chunks, batch...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected chunks")
	}
	for _, ch := range chunks {
		if ch.ChunkStrategy == "line_window" {
			t.Fatalf(".js with JSX must use AST, got line_window: %+v", ch)
		}
	}
	found := false
	for _, ch := range chunks {
		if ch.SymbolName == "App" && ch.SymbolKind == "function" {
			found = true
		}
	}
	if !found {
		t.Fatalf("chunks=%+v", chunks)
	}
}
