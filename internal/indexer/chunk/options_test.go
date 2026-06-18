package chunk

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestOptionsFromConfig(t *testing.T) {
	opts := OptionsFromConfig(config.IndexingConfig{
		Chunking: config.ChunkingConfig{
			Strategy:       "hybrid",
			MinChunkTokens: 5,
			ContextPrefix:  true,
			LineWindow:     config.LineWindowConfig{WindowLines: 10, OverlapLines: 2},
		},
		MaxFileBytes: 1000,
	}, config.EmbeddingProfile{MaxInputTokens: 512, EmbedBudgetRatio: 0.5})
	if opts.ChunkingStrategy != "hybrid" || opts.WindowLines != 10 || opts.MaxInputTokens != 512 {
		t.Fatalf("opts=%+v", opts)
	}
}

func TestBodyBudgetWithPrefix(t *testing.T) {
	opts := Options{MaxInputTokens: 100, EmbedBudgetRatio: 1.0, ContextPrefix: true}
	b := opts.BodyBudget(token.CharCounter{}, "a.go", "package main")
	if b >= 100 {
		t.Fatalf("budget should subtract prefix: %d", b)
	}
}

func TestRouterLineWindowStrategy(t *testing.T) {
	r := NewChunkRouter()
	var n int
	err := r.ChunkFile("x.txt", []byte("hello\nworld\n"), Options{ChunkingStrategy: "line_window", WindowLines: 1}, token.CharCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected chunks")
	}
}

func TestEmbedTextForChunk(t *testing.T) {
	got := EmbedTextForChunk("a.go", "package main", "code", true)
	if got == "code" {
		t.Fatal("expected prefix")
	}
}
