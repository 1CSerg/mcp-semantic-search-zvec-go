//go:build zvec && treesitter

package chunk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestChunkSizeLimits(t *testing.T) {
	maxTokens := 512
	opts := Options{
		ChunkingStrategy:  "hybrid",
		MaxInputTokens:    maxTokens,
		EmbedBudgetRatio:  0.9,
		MinChunkTokens:    1,
		ContextPrefix:     true,
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
	counter := token.CharCounter{}
	root := filepath.Join("testdata", "integration", "minirepo")
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		_, err = ProcessBatches(root, rel, opts, counter, 16, func(batch []zvec.Chunk) error {
			for _, ch := range batch {
				bodyBudget := opts.BodyBudget(counter, ch.RelativePath, ch.ParentScope)
				if counter.Count(ch.Snippet) > bodyBudget {
					t.Errorf("%s: body tokens %d exceed budget %d (strategy=%s kind=%s)", rel, counter.Count(ch.Snippet), bodyBudget, ch.ChunkStrategy, ch.SymbolKind)
				}
				embedText := EmbedTextForChunk(ch.RelativePath, ch.ParentScope, ch.Snippet, opts.ContextPrefix)
				maxEmbed := int(float64(opts.MaxInputTokens) * opts.EmbedBudgetRatio)
				if maxEmbed <= 0 {
					maxEmbed = opts.MaxInputTokens
				}
				if counter.Count(embedText) > maxEmbed {
					t.Errorf("%s: embed tokens %d exceed embed budget %d", rel, counter.Count(embedText), maxEmbed)
				}
				if strings.TrimSpace(ch.Snippet) == "" {
					t.Errorf("%s: empty snippet", rel)
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
