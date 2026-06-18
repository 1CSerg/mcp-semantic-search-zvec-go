//go:build !zvec || !treesitter

package chunk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestBuildTags_NoTreeSitter(t *testing.T) {
	root := filepath.Join("testdata", "integration", "minirepo")
	opts := Options{
		ChunkingStrategy: "hybrid",
		MaxInputTokens:   256,
		EmbedBudgetRatio: 1.0,
		MinChunkTokens:   1,
		WindowLines:      5,
		OverlapLines:     1,
		Languages: map[string]config.LanguageConfig{
			"go":         {Enabled: true},
			"python":     {Enabled: true},
			"javascript": {Enabled: true},
			"typescript": {Enabled: true},
			"bsl":        {Enabled: true, IncludeSDBL: true},
		},
	}
	counter := token.CharCounter{}
	total := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		n, err := ProcessBatches(root, rel, opts, counter, 8, func(batch []zvec.Chunk) error {
			for _, ch := range batch {
				ext := filepath.Ext(rel)
				switch ext {
				case ".md", ".markdown", ".mdc", ".txt":
					if ch.ChunkStrategy != "prose" && ch.ChunkStrategy != "partial" {
						t.Errorf("%s: expected prose strategy without treesitter, got %q", rel, ch.ChunkStrategy)
					}
				default:
					if ch.ChunkStrategy != "line_window" && ch.ChunkStrategy != "partial" {
						t.Errorf("%s: expected line_window without treesitter, got %q", rel, ch.ChunkStrategy)
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		total += n
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total == 0 {
		t.Fatal("expected chunks from minirepo without treesitter")
	}
}
