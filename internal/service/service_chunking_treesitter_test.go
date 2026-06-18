//go:build zvec && treesitter

package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

type chunkSearchCase struct {
	query      string
	pathSuffix string
	symbolName string
	symbolKind string
	strategy   string
}

func TestSemanticSearch_MinirepoHybridE2E(t *testing.T) {
	root := t.TempDir()
	copyChunkMinirepoTo(t, root)
	indexDir := t.TempDir()

	srv := modelsEmbedServer(t)
	defer srv.Close()

	settings := hybridChunkingSettings(t, root, indexDir, srv.URL, 1)
	profile := settings.App.Profiles["test"]
	store := newKeywordSearchStore()
	embed := &keywordTestEmbedder{dims: profile.Dimensions}
	zcfg := zvec.Config{
		IndexDir: indexDir, WorkspaceRoot: root, ProfileName: settings.App.ActiveProfile, Dimensions: profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, embed, store, zcfg)
	p := newPhase1ChunkingTest(settings, embed, store, coord, zcfg)
	runPhase1Reindex(t, p, true)

	cases := []chunkSearchCase{
		{query: "auth middleware", pathSuffix: "middleware.go", symbolName: "AuthMiddleware", symbolKind: "function", strategy: "ast"},
		{query: "Процедура Привет", pathSuffix: "Processing.bsl", symbolName: "Привет", symbolKind: "procedure", strategy: "ast"},
		{query: "встроенный запрос остатки", pathSuffix: "Processing.bsl", symbolKind: "query", strategy: "ast"},
		{query: "пакет запросов отчёт", pathSuffix: "Report.dcs", symbolKind: "query", strategy: "ast"},
		{query: "React button component", pathSuffix: "Button.tsx", symbolName: "Button", symbolKind: "function", strategy: "ast"},
		{query: "legacy button", pathSuffix: "LegacyButton.js", strategy: "line_window"},
		{query: "System architecture", pathSuffix: "architecture.md", symbolKind: "section", strategy: "prose"},
		{query: "changelog version table", pathSuffix: "changelog.md", strategy: "prose"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			raw, err := p.SemanticSearch(context.Background(), SearchRequest{Query: tc.query, Limit: 5})
			if err != nil {
				t.Fatal(err)
			}
			assertSearchResult(t, raw, tc.pathSuffix, tc.symbolName, tc.symbolKind, tc.strategy)
		})
	}
}

func TestSharedDaemon_MultiTenant_Chunking_AST(t *testing.T) {
	root := t.TempDir()
	wsA := filepath.Join(root, "ws-a")
	wsB := filepath.Join(root, "ws-b")
	goSource := []byte("package main\n\nfunc TenantFunc() int {\n\treturn 42\n}\n")
	for _, ws := range []string{wsA, wsB} {
		if err := os.MkdirAll(ws, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ws, "main.go"), goSource, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	indexDirA := filepath.Join(wsA, "index")
	indexDirB := filepath.Join(wsB, "index")
	if indexDirA == indexDirB {
		t.Fatal("tenants must use separate index_dir")
	}

	settingsA := &config.Settings{
		WorkspaceRoot: wsA,
		WorkspaceID:   "tenant-a",
		IndexDir:      indexDirA,
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible", Model: "m", Dimensions: 64, MaxInputTokens: 512, EmbedBudgetRatio: 1.0, BatchSize: 8},
			},
			Indexing: config.IndexingConfig{
				Extensions: []string{".go"},
				Chunking: config.ChunkingConfig{
					Strategy: "hybrid",
					Version:  1,
					Languages: map[string]config.LanguageConfig{
						"go": {Enabled: true},
					},
				},
			},
		},
	}
	settingsB := &config.Settings{
		WorkspaceRoot: wsB,
		WorkspaceID:   "tenant-b",
		IndexDir:      indexDirB,
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible", Model: "m", Dimensions: 64, MaxInputTokens: 512, EmbedBudgetRatio: 1.0, BatchSize: 8},
			},
			Indexing: config.IndexingConfig{
				Extensions: []string{".go"},
				Chunking: config.ChunkingConfig{
					Strategy:   "line_window",
					Version:    1,
					LineWindow: config.LineWindowConfig{WindowLines: 5, OverlapLines: 1},
				},
			},
		},
	}

	storeA := newKeywordSearchStore()
	storeB := newKeywordSearchStore()
	embed := &keywordTestEmbedder{dims: 64}
	profile := settingsA.App.Profiles["test"]

	coordA := indexer.NewCoordinator(settingsA, profile, embed, storeA, zvec.Config{
		IndexDir: indexDirA, WorkspaceRoot: wsA, ProfileName: "test", Dimensions: 64,
	})
	coordB := indexer.NewCoordinator(settingsB, profile, embed, storeB, zvec.Config{
		IndexDir: indexDirB, WorkspaceRoot: wsB, ProfileName: "test", Dimensions: 64,
	})
	pA := newPhase1ChunkingTest(settingsA, embed, storeA, coordA, zvec.Config{
		IndexDir: indexDirA, WorkspaceRoot: wsA, ProfileName: "test", Dimensions: 64,
	})
	pB := newPhase1ChunkingTest(settingsB, embed, storeB, coordB, zvec.Config{
		IndexDir: indexDirB, WorkspaceRoot: wsB, ProfileName: "test", Dimensions: 64,
	})
	runPhase1Reindex(t, pA, true)
	runPhase1Reindex(t, pB, true)

	strategiesA := map[string]int{}
	for _, ch := range storeA.storedChunks() {
		strategiesA[ch.ChunkStrategy]++
	}
	strategiesB := map[string]int{}
	for _, ch := range storeB.storedChunks() {
		strategiesB[ch.ChunkStrategy]++
	}
	if strategiesA["ast"] == 0 {
		t.Fatalf("tenant A expected ast chunks, got %v", strategiesA)
	}
	if strategiesB["line_window"] == 0 {
		t.Fatalf("tenant B expected line_window chunks, got %v", strategiesB)
	}
	if strategiesB["ast"] > 0 {
		t.Fatalf("tenant B should not use ast strategy: %v", strategiesB)
	}

	rawA, err := pA.SemanticSearch(context.Background(), SearchRequest{Query: "return 42 main"})
	if err != nil {
		t.Fatal(err)
	}
	rawB, err := pB.SemanticSearch(context.Background(), SearchRequest{Query: "return 42 main"})
	if err != nil {
		t.Fatal(err)
	}
	assertSearchResult(t, rawA, "main.go", "TenantFunc", "function", "ast")
	assertSearchResult(t, rawB, "main.go", "", "", "line_window")
}
