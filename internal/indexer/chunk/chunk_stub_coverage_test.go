package chunk

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestResolveWithinRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, err := ResolveWithinRoot(root, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("stat resolved path: %v", err)
	}
}

func TestNormalizeWindowLinesDefaults(t *testing.T) {
	window, overlap := NormalizeWindowLines(0, 0)
	if window != defaultWindowLines || overlap != defaultOverlapLines {
		t.Fatalf("window=%d overlap=%d", window, overlap)
	}
}

func TestHybridASTPath(t *testing.T) {
	hybridGo := Options{
		ChunkingStrategy: "hybrid",
		Languages:        map[string]config.LanguageConfig{"go": {Enabled: true}},
	}
	if !hybridASTPath("pkg/main.go", hybridGo) {
		t.Fatal("expected go file on hybrid AST path")
	}
	if hybridASTPath("pkg/main.go", Options{ChunkingStrategy: "line_window"}) {
		t.Fatal("line_window strategy should skip hybrid AST path")
	}
	dcsOpts := Options{
		ChunkingStrategy: "hybrid",
		Languages:        map[string]config.LanguageConfig{"bsl": {Enabled: true, IncludeSDBL: true}},
	}
	if !hybridASTPath("reports/schema.dcs", dcsOpts) {
		t.Fatal("expected .dcs on hybrid AST path when include_sdbl=true")
	}
	if hybridASTPath("reports/schema.dcs", Options{ChunkingStrategy: "hybrid"}) {
		t.Fatal("expected .dcs off hybrid AST path when include_sdbl=false")
	}
}

func TestProcessBatchesEmitError(t *testing.T) {
	root := t.TempDir()
	rel := "a.go"
	if err := os.WriteFile(filepath.Join(root, rel), []byte("package main\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("batch emit failed")
	_, err := ProcessBatches(root, rel, Options{WindowLines: 2, ChunkingStrategy: "line_window"}, token.CharCounter{}, 1, func([]zvec.Chunk) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want %v", err, wantErr)
	}
}

func TestOptionsFromConfigDefaults(t *testing.T) {
	opts := OptionsFromConfig(config.IndexingConfig{}, config.EmbeddingProfile{})
	if opts.WindowLines != defaultWindowLines || opts.OverlapLines != defaultOverlapLines {
		t.Fatalf("defaults not applied: %+v", opts)
	}
	if opts.Languages == nil {
		t.Fatal("expected non-nil languages map")
	}
}
