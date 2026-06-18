package chunk

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestRouterProseMarkdown(t *testing.T) {
	r := NewChunkRouter()
	content := []byte("# Title\n\nBody paragraph.\n")
	var chunks []zvec.Chunk
	err := r.ChunkFile("README.md", content, Options{
		ChunkingStrategy:  "hybrid",
		MaxInputTokens:    120,
		EmbedBudgetRatio:  1.0,
		ProseOverlapRatio: 0.12,
		MinChunkTokens:    1,
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
		t.Fatal("expected prose chunks")
	}
	for _, ch := range chunks {
		if ch.ChunkStrategy != "prose" && ch.ChunkStrategy != "partial" {
			t.Fatalf("strategy=%q", ch.ChunkStrategy)
		}
		if ch.ChunkType != "markdown" {
			t.Fatalf("chunk_type=%q", ch.ChunkType)
		}
	}
}

func TestRouterProseMDC(t *testing.T) {
	r := NewChunkRouter()
	content := []byte("---\nkey: v\n---\n\n# Rule\n\nText.\n")
	var chunks []zvec.Chunk
	err := r.ChunkFile(".cursor/rules/x.mdc", content, Options{
		ChunkingStrategy: "hybrid",
		MaxInputTokens:   120,
		EmbedBudgetRatio: 1.0,
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
		t.Fatal("expected chunks")
	}
	if chunks[0].ChunkType != "markdown" {
		t.Fatalf("chunk_type=%q want markdown", chunks[0].ChunkType)
	}
}

func TestHybridProsePath(t *testing.T) {
	opts := Options{ChunkingStrategy: "hybrid"}
	if !hybridProsePath("doc.md", opts) {
		t.Fatal("md should be prose path")
	}
	if hybridProsePath("main.go", opts) {
		t.Fatal("go should not be prose path")
	}
	if hybridProsePath("doc.md", Options{ChunkingStrategy: "line_window"}) {
		t.Fatal("line_window should not use prose")
	}
}
