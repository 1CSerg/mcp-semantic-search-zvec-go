//go:build zvec && treesitter

package ast

import (
	"fmt"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_bsl "github.com/tree-sitter/tree-sitter-bsl/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

var (
	goLanguage         *sitter.Language
	goLanguageOnce     sync.Once
	pythonLanguage     *sitter.Language
	pythonLanguageOnce sync.Once
	jsLanguage         *sitter.Language
	jsLanguageOnce     sync.Once
	tsLanguage         *sitter.Language
	tsLanguageOnce     sync.Once
	tsxLanguage        *sitter.Language
	tsxLanguageOnce    sync.Once
	bslLanguage        *sitter.Language
	bslLanguageOnce    sync.Once
)

func goLang() *sitter.Language {
	goLanguageOnce.Do(func() {
		goLanguage = sitter.NewLanguage(tree_sitter_go.Language())
	})
	return goLanguage
}

func pythonLang() *sitter.Language {
	pythonLanguageOnce.Do(func() {
		pythonLanguage = sitter.NewLanguage(tree_sitter_python.Language())
	})
	return pythonLanguage
}

func javascriptLang() *sitter.Language {
	jsLanguageOnce.Do(func() {
		jsLanguage = sitter.NewLanguage(tree_sitter_javascript.Language())
	})
	return jsLanguage
}

func typescriptLang() *sitter.Language {
	tsLanguageOnce.Do(func() {
		tsLanguage = sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())
	})
	return tsLanguage
}

func tsxLang() *sitter.Language {
	tsxLanguageOnce.Do(func() {
		tsxLanguage = sitter.NewLanguage(tree_sitter_typescript.LanguageTSX())
	})
	return tsxLanguage
}

func bslLang() *sitter.Language {
	bslLanguageOnce.Do(func() {
		bslLanguage = sitter.NewLanguage(tree_sitter_bsl.Language())
	})
	return bslLanguage
}

type parserPool struct {
	lang *sitter.Language
	pool sync.Pool
	// created tracks every parser ever handed out by the pool so they can be
	// closed on shutdown. sync.Pool may silently evict entries (especially under
	// GC pressure), and the tree-sitter Go bindings have NO finalizer for the
	// underlying C TSParser — see the binding README: "you must always call
	// Close". Without this list, evicted parsers would leak C memory. The slice
	// is append-only and guarded by createdMu; the parsers it holds are never
	// read concurrently with parse (Close runs only after all parsing is done).
	createdMu sync.Mutex
	created   []*sitter.Parser
}

func newParserPool(lang *sitter.Language) *parserPool {
	pp := &parserPool{lang: lang}
	pp.pool = sync.Pool{
		New: func() any {
			p := sitter.NewParser()
			if err := p.SetLanguage(lang); err != nil {
				// SetLanguage only fails on an incompatible language, which is a
				// build-time property; panic mirrors the previous eager behaviour
				// so the failure surfaces immediately rather than on first parse.
				p.Close()
				panic(fmt.Sprintf("tree-sitter SetLanguage: %v", err))
			}
			pp.createdMu.Lock()
			pp.created = append(pp.created, p)
			pp.createdMu.Unlock()
			return p
		},
	}
	return pp
}

func (p *parserPool) borrow() *sitter.Parser {
	return p.pool.Get().(*sitter.Parser)
}

func (p *parserPool) release(parser *sitter.Parser) {
	p.pool.Put(parser)
}

// closeAll releases the native C TSParser handle of every parser this pool ever
// allocated. It must be called only when no more parsing will happen (process
// shutdown). After closeAll the pool must not be used again.
func (p *parserPool) closeAll() {
	p.createdMu.Lock()
	defer p.createdMu.Unlock()
	for _, parser := range p.created {
		parser.Close()
	}
	p.created = nil
}

// parseTree parses source with a pooled parser. Caller must call tree.Close() and then release parser.
func (p *parserPool) parseTree(src []byte) (*sitter.Parser, *sitter.Tree, error) {
	parser := p.borrow()
	tree := parser.Parse(src, nil)
	if tree == nil {
		p.release(parser)
		return nil, nil, errParseFailed
	}
	return parser, tree, nil
}

var goParserPool = newParserPool(goLang())

func parserPoolForLang(lang string) *parserPool {
	spec, ok := grammars[lang]
	if !ok || spec.pool == nil {
		return goParserPool
	}
	return spec.pool
}
