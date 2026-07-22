package prose

import (
	"math"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

// Config drives prose and markdown chunking budgets.
type Config struct {
	MaxInputTokens    int
	EmbedBudgetRatio  float64
	ProseOverlapRatio float64
	ContextPrefix     bool
	MinChunkTokens    int
	WindowLines       int
	OverlapLines      int
}

// EmitFunc streams each produced chunk.
type EmitFunc func(*zvec.Chunk) error

func (c Config) bodyBudget(counter token.TokenCounter, rel, parentScope string) int {
	total := int(math.Floor(float64(c.MaxInputTokens) * c.EmbedBudgetRatio))
	if total <= 0 {
		total = c.MaxInputTokens
	}
	if !c.ContextPrefix || counter == nil {
		return total
	}
	prefix := contextPrefix(rel, parentScope)
	remain := total - counter.Count(prefix)
	if remain < 1 {
		return 1
	}
	return remain
}

func (c Config) overlapTokens(budget int) int {
	ratio := c.ProseOverlapRatio
	if ratio <= 0 {
		ratio = 0.12
	}
	n := int(math.Floor(float64(budget) * ratio))
	if n < 1 && budget > 1 {
		return 1
	}
	return n
}

func contextPrefix(relativePath, parentScope string) string {
	rel := strings.ReplaceAll(relativePath, "\\", "/")
	if parentScope == "" {
		return "// file: " + rel + "\n"
	}
	return "// file: " + rel + "\n// scope: " + parentScope + "\n"
}

func chunkTypeForPath(rel string) string {
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".md", ".markdown", ".mdc", ".txt":
		return "markdown"
	default:
		return "markdown"
	}
}

func normalizeWindow(window, overlap int) (int, int) {
	if window <= 0 {
		window = 40
	}
	if overlap <= 0 {
		overlap = 8
	}
	if overlap >= window {
		overlap = window / 4
	}
	return window, overlap
}
