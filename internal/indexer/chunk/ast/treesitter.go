//go:build zvec && treesitter

package ast

import (
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
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

type parserPool struct {
	pool sync.Pool
}

func newParserPool(lang *sitter.Language) *parserPool {
	return &parserPool{
		pool: sync.Pool{
			New: func() any {
				p := sitter.NewParser()
				p.SetLanguage(lang)
				return p
			},
		},
	}
}

func (p *parserPool) borrow() *sitter.Parser {
	return p.pool.Get().(*sitter.Parser)
}

func (p *parserPool) release(parser *sitter.Parser) {
	p.pool.Put(parser)
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
