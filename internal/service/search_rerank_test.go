package service

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestRerankSearchHitsPrefersPathMatch(t *testing.T) {
	hits := []zvec.SearchHit{
		{Path: "docs/guide.md", Score: 0.31, ChunkStrategy: "prose"},
		{Path: "backend/auth/middleware.go", Score: 0.66, ChunkStrategy: "ast"},
	}
	out := rerankSearchHits(hits, "authentication middleware gate")
	if len(out) < 2 {
		t.Fatal("expected reranked hits")
	}
	if out[0].Path != "backend/auth/middleware.go" {
		t.Fatalf("first hit path=%q want middleware.go", out[0].Path)
	}
}

func TestRerankSearchHitsPreservesOrderWhenNoTerms(t *testing.T) {
	hits := []zvec.SearchHit{
		{Path: "a.go", Score: 0.1},
		{Path: "b.go", Score: 0.2},
	}
	out := rerankSearchHits(hits, "  ")
	if out[0].Path != "a.go" || out[1].Path != "b.go" {
		t.Fatalf("unexpected reorder: %+v", out)
	}
}
