package chunk

import (
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

// Options configures chunk splitting and hybrid AST routing.
type Options struct {
	WindowLines          int
	OverlapLines         int
	MaxFileBytes         int64
	StreamThresholdBytes int64
	MaxLineBytes         int64

	ChunkingStrategy  string
	MinChunkTokens    int
	ProseOverlapRatio float64
	ContextPrefix     bool
	MaxInputTokens    int
	EmbedBudgetRatio  float64
	Languages         map[string]config.LanguageConfig
}

// OptionsFromConfig builds chunk.Options from indexing config and the active embedding profile.
func OptionsFromConfig(idx config.IndexingConfig, profile config.EmbeddingProfile) Options {
	lw := idx.Chunking.LineWindow
	if lw.WindowLines == 0 {
		lw.WindowLines = defaultWindowLines
	}
	if lw.OverlapLines == 0 {
		lw.OverlapLines = defaultOverlapLines
	}
	langs := idx.Chunking.Languages
	if langs == nil {
		langs = map[string]config.LanguageConfig{}
	}
	return Options{
		WindowLines:          lw.WindowLines,
		OverlapLines:         lw.OverlapLines,
		MaxFileBytes:         idx.MaxFileBytes,
		StreamThresholdBytes: idx.StreamChunkThresholdBytes,
		MaxLineBytes:         idx.MaxLineBytes,
		ChunkingStrategy:     idx.Chunking.Strategy,
		MinChunkTokens:       idx.Chunking.MinChunkTokens,
		ProseOverlapRatio:    idx.Chunking.ProseOverlapRatio,
		ContextPrefix:        idx.Chunking.ContextPrefix,
		MaxInputTokens:       profile.MaxInputTokens,
		EmbedBudgetRatio:     profile.EmbedBudgetRatio,
		Languages:            langs,
	}
}

// LanguageEnabled reports whether AST chunking is enabled for a language key (e.g. "go").
func (o Options) LanguageEnabled(lang string) bool {
	cfg, ok := o.Languages[lang]
	if !ok {
		return false
	}
	return cfg.Enabled
}

// BodyBudget returns the token budget for chunk body text after optional context prefix.
func (o Options) BodyBudget(counter interface{ Count(string) int }, relativePath, parentScope string) int {
	total := int(float64(o.MaxInputTokens) * o.EmbedBudgetRatio)
	if total <= 0 {
		total = o.MaxInputTokens
	}
	if !o.ContextPrefix || counter == nil {
		return total
	}
	prefix := FormatContextPrefix(relativePath, parentScope)
	remain := total - counter.Count(prefix)
	if remain < 1 {
		return 1
	}
	return remain
}

// FormatContextPrefix builds the embed-only context header (not stored in zvec snippet).
func FormatContextPrefix(relativePath, parentScope string) string {
	rel := strings.ReplaceAll(relativePath, "\\", "/")
	if parentScope == "" {
		return "// file: " + rel + "\n"
	}
	return "// file: " + rel + "\n// scope: " + parentScope + "\n"
}

// FormatEmbedText prepends context prefix to snippet for embedding when enabled.
func FormatEmbedText(ch interface {
	GetRelativePath() string
	GetParentScope() string
	GetSnippet() string
}, contextPrefix bool) string {
	if !contextPrefix {
		return ch.GetSnippet()
	}
	prefix := FormatContextPrefix(ch.GetRelativePath(), ch.GetParentScope())
	return prefix + ch.GetSnippet()
}

// chunkEmbedFields adapts zvec.Chunk for FormatEmbedText without importing zvec in tests.
type chunkEmbedFields struct {
	rel, scope, snippet string
}

func (c chunkEmbedFields) GetRelativePath() string { return c.rel }
func (c chunkEmbedFields) GetParentScope() string  { return c.scope }
func (c chunkEmbedFields) GetSnippet() string      { return c.snippet }

// EmbedTextForChunk returns text sent to the embedding model for a zvec chunk.
func EmbedTextForChunk(relativePath, parentScope, snippet string, contextPrefix bool) string {
	return FormatEmbedText(chunkEmbedFields{rel: relativePath, scope: parentScope, snippet: snippet}, contextPrefix)
}
