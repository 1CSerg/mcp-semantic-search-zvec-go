//go:build zvec && treesitter

package ast

import (
	"testing"
)

func TestParserPoolCloseAll(t *testing.T) {
	pool := newParserPool(goLang())
	parser, tree, err := pool.parseTree([]byte("package main\nfunc main() {}\n"))
	if err != nil {
		t.Fatalf("parseTree: %v", err)
	}
	tree.Close()
	pool.release(parser)

	pool.closeAll()
	pool.closeAll() // idempotent after created slice is cleared
}

func TestGrammarSpecCloseQueryLoaded(t *testing.T) {
	spec := &grammarSpec{
		language: goLang(),
		querySrc: goQuerySource,
		pool:     newParserPool(goLang()),
	}
	if _, err := spec.loadQuery(); err != nil {
		t.Fatalf("loadQuery: %v", err)
	}
	spec.closeQuery()
	if spec.query != nil {
		t.Fatal("query should be nil after closeQuery")
	}
}

func TestGrammarSpecCloseQueryWithoutLoad(t *testing.T) {
	spec := &grammarSpec{
		language: goLang(),
		querySrc: goQuerySource,
		pool:     newParserPool(goLang()),
	}
	spec.closeQuery()
}

func TestCloseResourcesForLocalInstances(t *testing.T) {
	specs := map[string]*grammarSpec{
		"test": {
			language: goLang(),
			querySrc: goQuerySource,
			pool:     newParserPool(goLang()),
		},
	}
	parser, tree, err := specs["test"].pool.parseTree([]byte("package main\nfunc main() {}\n"))
	if err != nil {
		t.Fatalf("parseTree: %v", err)
	}
	tree.Close()
	specs["test"].pool.release(parser)
	if _, err := specs["test"].loadQuery(); err != nil {
		t.Fatalf("loadQuery: %v", err)
	}
	closeResourcesFor(specs)
}
