//go:build zvec && treesitter

package ast

import (
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

var (
	goLanguage     *sitter.Language
	goLanguageOnce sync.Once
)

func goLang() *sitter.Language {
	goLanguageOnce.Do(func() {
		goLanguage = sitter.NewLanguage(tree_sitter_go.Language())
	})
	return goLanguage
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
