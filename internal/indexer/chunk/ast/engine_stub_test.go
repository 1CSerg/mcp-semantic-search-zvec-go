//go:build !zvec || !treesitter

package ast

import (
	"errors"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestChunkStubsReturnErrNotImplemented(t *testing.T) {
	cfg := Config{WindowLines: 10, OverlapLines: 2, MaxInputTokens: 100}
	counter := token.CharCounter{}
	emit := func(*zvec.Chunk) error { return nil }
	src := []byte("package main\n")

	stubs := []struct {
		name string
		fn   func() error
	}{
		{"ChunkGo", func() error { return ChunkGo("main.go", src, cfg, counter, emit) }},
		{"ChunkPython", func() error { return ChunkPython("main.py", src, cfg, counter, emit) }},
		{"ChunkJavaScript", func() error { return ChunkJavaScript("main.js", src, cfg, counter, emit) }},
		{"ChunkTypeScript", func() error { return ChunkTypeScript("main.ts", src, cfg, counter, emit) }},
		{"ChunkTSX", func() error { return ChunkTSX("main.tsx", src, cfg, counter, emit) }},
		{"ChunkBSL", func() error { return ChunkBSL("main.bsl", src, cfg, counter, emit) }},
		{"ChunkLanguage", func() error { return ChunkLanguage("go", "main.go", src, cfg, counter, emit) }},
	}
	for _, tc := range stubs {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); !errors.Is(err, ErrNotImplemented) {
				t.Fatalf("err=%v want ErrNotImplemented", err)
			}
		})
	}
}

func TestNormalizeWindow(t *testing.T) {
	window, overlap := normalizeWindow(0, 0)
	if window != 40 || overlap != 8 {
		t.Fatalf("defaults window=%d overlap=%d", window, overlap)
	}
	window, overlap = normalizeWindow(20, 25)
	if window != 20 || overlap != 5 {
		t.Fatalf("clamp overlap window=%d overlap=%d", window, overlap)
	}
}

func TestEmitPartialWindows(t *testing.T) {
	cfg := Config{
		MinChunkTokens:   1,
		MaxInputTokens:   200,
		EmbedBudgetRatio: 1,
		WindowLines:      2,
		OverlapLines:     0,
	}
	counter := token.CharCounter{}
	var chunks []*zvec.Chunk
	emit := func(ch *zvec.Chunk) error {
		chunks = append(chunks, ch)
		return nil
	}
	lines := []string{"line one", "line two", "line three"}
	meta := partialMeta{chunkStrategy: "line_window", symbolKind: "file", parentScope: "module x"}
	if err := emitPartialWindows("pkg/a.go", lines, 1, cfg, counter, meta, emit); err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	if !strings.Contains(chunks[0].Snippet, "line one") {
		t.Fatalf("snippet=%q", chunks[0].Snippet)
	}
}

func TestScopeStringAllKinds(t *testing.T) {
	s := Scope{Segments: []ScopeSegment{
		{Kind: "package", Name: "main"},
		{Kind: "type", Name: "Server"},
		{Kind: "func", Name: "Run"},
		{Kind: "function", Name: "fn"},
		{Kind: "method", Name: "m"},
		{Kind: "class", Name: "C"},
		{Kind: "module", Name: "mod"},
		{Kind: "region", Name: "R"},
		{Kind: "custom", Name: "x"},
	}}
	got := s.String()
	for _, want := range []string{
		"package main", "type Server", "func Run", "function fn",
		"method m", "class C", "module mod", "region R", "custom x",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("String()=%q missing %q", got, want)
		}
	}
}

func TestPackageAndModuleScopeEmpty(t *testing.T) {
	if PackageScope("").String() != "" {
		t.Fatal("empty package scope")
	}
	if ModuleScope("").String() != "" {
		t.Fatal("empty module scope")
	}
}

func TestWithSegmentSkipsEmptyName(t *testing.T) {
	base := PackageScope("main")
	if base.WithSegment("func", "").String() != "package main" {
		t.Fatal("WithSegment should skip empty name")
	}
}

func TestContextPrefix(t *testing.T) {
	if got := contextPrefix("dir\\file.go", ""); got != "// file: dir/file.go\n" {
		t.Fatalf("contextPrefix=%q", got)
	}
	if got := contextPrefix("a.go", "package main"); !strings.Contains(got, "// scope: package main") {
		t.Fatalf("contextPrefix=%q", got)
	}
}
