//go:build !zvec || !treesitter

package chunk

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestRouterGoFallbackWithoutTreesitter(t *testing.T) {
	r := NewChunkRouter()
	var chunks []zvec.Chunk
	err := r.ChunkFile("main.go", []byte("package main\n\nfunc F() {}\n"), Options{
		ChunkingStrategy: "hybrid",
		MinChunkTokens:   1,
		MaxInputTokens:   256,
		EmbedBudgetRatio: 1.0,
		WindowLines:      5,
		OverlapLines:     1,
		Languages: map[string]config.LanguageConfig{
			"go": {Enabled: true},
		},
	}, token.CharCounter{}, func(ch *zvec.Chunk) error {
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
			t.Fatalf("expected line_window fallback without treesitter tag, got %q", ch.ChunkStrategy)
		}
	}
}
