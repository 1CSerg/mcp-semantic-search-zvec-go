package chunk

import (
	"errors"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/ast"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/prose"
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
	".bsl": "bsl",
	".os":  "bsl",
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
	if prose.IsProsePath(relativePath) && proseStrategy(opts) {
		proseEmit := func(ch *zvec.Chunk) error { return emit(ch) }
		return prose.ChunkFile(relativePath, content, proseConfig(opts), counter, proseEmit)
	}
	ext := strings.ToLower(filepath.Ext(relativePath))
	if ext == ".dcs" && opts.bslIncludeSDBL() {
		return r.chunkDCS(relativePath, content, opts, counter, emit)
	}
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

func (o Options) bslIncludeSDBL() bool {
	cfg, ok := o.Languages["bsl"]
	return ok && cfg.Enabled && cfg.IncludeSDBL
}

func astConfig(opts Options) ast.Config {
	return ast.Config{
		MinChunkTokens:   opts.MinChunkTokens,
		MaxInputTokens:   opts.MaxInputTokens,
		EmbedBudgetRatio: opts.EmbedBudgetRatio,
		ContextPrefix:    opts.ContextPrefix,
		WindowLines:      opts.WindowLines,
		OverlapLines:     opts.OverlapLines,
		IncludeSDBL:      opts.bslIncludeSDBL(),
	}
}

func (r *ChunkRouter) chunkDCS(relativePath string, content []byte, opts Options, counter token.TokenCounter, emit EmitFunc) error {
	cfg := astConfig(opts)
	queries := ast.ExtractDCSQueriesWithLines(content)
	moduleName := strings.TrimSuffix(filepath.Base(relativePath), filepath.Ext(relativePath))
	parentScope := "module " + moduleName
	for _, q := range queries {
		astEmit := func(ch *zvec.Chunk) error { return emit(ch) }
		if err := ast.ChunkSDBLText(relativePath, q.Text, q.StartLine, cfg, counter, parentScope, astEmit); err != nil {
			slog.Debug("chunk_fallback", "path", relativePath, "reason", err.Error(), "kind", "dcs_query")
			if fbErr := emitDCSQueryLineWindowFallback(relativePath, q, parentScope, opts, counter, emit); fbErr != nil {
				return fbErr
			}
		}
	}
	remaining := ast.StripDCSQueryBlocks(content)
	return FileChunksEmit(relativePath, remaining, opts, emit)
}

func emitDCSQueryLineWindowFallback(relativePath string, q ast.DCSQuery, parentScope string, opts Options, counter token.TokenCounter, emit EmitFunc) error {
	lines := strings.Split(q.Text, "\n")
	window, overlap := normalizeWindowOpts(opts)
	return SlideWindowEmit(relativePath, lines, q.StartLine, SlideWindowMeta{
		Window:        window,
		Overlap:       overlap,
		ChunkStrategy: "line_window",
		SymbolKind:    "query",
		ParentScope:   parentScope,
		Counter:       counter,
		MinTokens:     0,
	}, func(ch *zvec.Chunk) error {
		if ch != nil {
			ch.ChunkType = "query"
		}
		return emit(ch)
	})
}

func proseStrategy(opts Options) bool {
	s := strings.ToLower(opts.ChunkingStrategy)
	return s == "hybrid" || s == "prose"
}

func proseConfig(opts Options) prose.Config {
	return prose.ConfigFromOptions(
		opts.MaxInputTokens,
		opts.EmbedBudgetRatio,
		opts.ProseOverlapRatio,
		opts.ContextPrefix,
		opts.MinChunkTokens,
		opts.WindowLines,
		opts.OverlapLines,
	)
}

func hybridProsePath(relativePath string, opts Options) bool {
	return prose.IsProsePath(relativePath) && proseStrategy(opts)
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
	if ext == ".dcs" && opts.bslIncludeSDBL() {
		return true
	}
	lang, ok := ExtToLang[ext]
	return ok && opts.astEnabledForLang(lang)
}
