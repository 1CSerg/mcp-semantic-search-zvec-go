package service

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

// Lightweight search-quality regressions (no LM Studio / zvec). Fixtures mirror
// dogfood failure modes: docs bias, micro-const noise, symbol-name queries.
func TestSearchQualityRerankFixtures(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		hits      []zvec.SearchHit
		wantFirst string
	}{
		{
			name:  "symbol ChunkRouter over micro const",
			query: "func ChunkRouter hybrid AST",
			hits: []zvec.SearchHit{
				{Path: "internal/indexer/chunk/ast/treesitter.go", Score: 0.28, ChunkStrategy: "ast", SymbolName: "tsxLanguage", StartLine: 26, EndLine: 26, Snippet: "tsxLanguage"},
				{Path: "internal/indexer/chunk/router.go", Score: 0.30, ChunkStrategy: "ast", SymbolName: "ChunkRouter", StartLine: 29, EndLine: 45, Snippet: "// ChunkRouter selects AST or line_window chunking.\ntype ChunkRouter struct{}\nfunc NewChunkRouter() *ChunkRouter { return &ChunkRouter{} }"},
			},
			wantFirst: "internal/indexer/chunk/router.go",
		},
		{
			name:  "registry code over architecture docs",
			query: "WorkspaceRegistry BorrowService discardHandle release",
			hits: []zvec.SearchHit{
				{Path: "docs/ARCHITECTURE.md", Score: 0.27, ChunkStrategy: "prose", Snippet: "WorkspaceRegistry lazy-opens workspaces; BorrowService tracks handlers; discardHandle closes."},
				{Path: "templates/daemon.yaml", Score: 0.28, ChunkStrategy: "line_window", StartLine: 1, EndLine: 16, Snippet: "max_open_workspaces: 10\nworkspaces:\n  - id: my-app"},
				{Path: "internal/daemon/registry.go", Score: 0.33, ChunkStrategy: "ast", SymbolName: "BorrowService", StartLine: 200, EndLine: 240, Snippet: "func (r *Registry) BorrowService(workspaceID string) (*service.Phase1, func(), error) {\n\treturn h.phase1, release, nil\n}"},
			},
			wantFirst: "internal/daemon/registry.go",
		},
		{
			name:  "dimensions test over docs config prose",
			query: "send dimensions parameter embeddings API",
			hits: []zvec.SearchHit{
				{Path: "docs/CONFIG.md", Score: 0.20, ChunkStrategy: "prose", Snippet: "MRL embedding models send profile.dimensions in the embed API when set."},
				{Path: "internal/embeddings/openai/client_test.go", Score: 0.35, ChunkStrategy: "ast", SymbolName: "TestEmbedBatchSendsDimensions", StartLine: 45, EndLine: 74, Snippet: "func TestEmbedBatchSendsDimensions(t *testing.T) {\n\tif gotBody[\"dimensions\"] != float64(1024) {\n\t\tt.Fatal(\"dimensions not sent\")\n\t}\n}"},
			},
			wantFirst: "internal/embeddings/openai/client_test.go",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := rerankSearchHits(tc.hits, tc.query)
			if len(out) == 0 {
				t.Fatal("empty results")
			}
			if out[0].Path != tc.wantFirst {
				t.Fatalf("first=%q want %q (order=%v)", out[0].Path, tc.wantFirst, pathsOf(out))
			}
		})
	}
}

func TestSearchQualityOverFetchThenTruncate(t *testing.T) {
	limit := 3
	fetch := searchOverFetchLimit(limit)
	if fetch <= limit {
		t.Fatalf("over-fetch %d must exceed limit %d", fetch, limit)
	}
	hits := make([]zvec.SearchHit, 0, fetch)
	for i := 0; i < fetch; i++ {
		hits = append(hits, zvec.SearchHit{
			Path:          "docs/noise.md",
			Score:         0.20 + float64(i)*0.001,
			ChunkStrategy: "prose",
			Snippet:       "generic documentation filler text about indexing and search quality",
		})
	}
	// Plant a strong code hit near the end of the over-fetched window.
	hits[fetch-1] = zvec.SearchHit{
		Path:          "internal/daemon/registry.go",
		Score:         0.45,
		ChunkStrategy: "ast",
		SymbolName:    "Registry",
		StartLine:     50,
		EndLine:       90,
		Snippet:       "type Registry struct {\n\topen map[string]*workspaceHandle\n\tmu sync.Mutex\n}",
	}
	ranked := rerankSearchHits(hits, "shared daemon WorkspaceRegistry")
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	if ranked[0].Path != "internal/daemon/registry.go" {
		t.Fatalf("after over-fetch+rerank+truncate first=%q want registry.go", ranked[0].Path)
	}
}

func pathsOf(hits []zvec.SearchHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Path
	}
	return out
}
