package chunk

import (
	"errors"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/ast"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

// ExtToLang maps file extensions to AST language keys.
var ExtToLang = map[string]string{
	".go":  "go",
	".py":  "python",
	".js":  "javascript",
	".jsx": "tsx",
	".mjs": "javascript",
	".cjs": "javascript",
	".ts":  "typescript",
	".tsx": "tsx",
}

// ChunkRouter selects AST or line_window chunking per file extension and config.
type ChunkRouter struct{}

// NewChunkRouter creates a chunk router.
func NewChunkRouter() *ChunkRouter {
	return &ChunkRouter{}
}

// ChunkFile streams chunks through emit; production path does not accumulate []Chunk.
func (r *ChunkRouter) ChunkFile(relativePath string, content []byte, opts Options, counter token.TokenCounter, emit EmitFunc) error {
	content = prepareContent(content)
	if opts.ChunkingStrategy == "line_window" {
		return FileChunksEmit(relativePath, content, opts, emit)
	}
	ext := strings.ToLower(filepath.Ext(relativePath))
	lang, ok := ExtToLang[ext]
	if ok && opts.astEnabledForLang(lang) {
		cfg := astConfig(opts)
		astEmit := func(ch *zvec.Chunk) error { return emit(ch) }
		if err := ast.ChunkLanguage(lang, relativePath, content, cfg, counter, astEmit); err == nil {
			return nil
		} else if shouldFallbackToLineWindow(err) {
			slog.Debug("chunk_fallback", "path", relativePath, "reason", err.Error())
			return FileChunksEmit(relativePath, content, opts, emit)
		} else {
			return err
		}
	}
	return FileChunksEmit(relativePath, content, opts, emit)
}

func (o Options) astEnabledForLang(lang string) bool {
	if o.LanguageEnabled(lang) {
		return true
	}
	if lang == "tsx" && o.LanguageEnabled("typescript") {
		return true
	}
	return false
}

func astConfig(opts Options) ast.Config {
	return ast.Config{
		MinChunkTokens:   opts.MinChunkTokens,
		MaxInputTokens:   opts.MaxInputTokens,
		EmbedBudgetRatio: opts.EmbedBudgetRatio,
		ContextPrefix:    opts.ContextPrefix,
		WindowLines:      opts.WindowLines,
		OverlapLines:     opts.OverlapLines,
	}
}

func shouldFallbackToLineWindow(err error) bool {
	return errors.Is(err, ast.ErrNotImplemented) ||
		errors.Is(err, ast.ErrEmptyTree) ||
		errors.Is(err, ast.ErrHighParseErrorRate)
}

// hybridASTPath reports whether hybrid strategy routes this file through in-memory AST
// chunking (whole-file read up to MaxFileBytes), not streaming line_window.
func hybridASTPath(relativePath string, opts Options) bool {
	if opts.ChunkingStrategy == "line_window" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(relativePath))
	lang, ok := ExtToLang[ext]
	return ok && opts.astEnabledForLang(lang)
}
