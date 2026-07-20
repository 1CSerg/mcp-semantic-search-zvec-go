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

func TestSearchQueryTermsMinRuneLength(t *testing.T) {
	// 2-rune Cyrillic tokens are 4 bytes; must be filtered like ASCII "to".
	terms := searchQueryTerms("на по to auth")
	for _, term := range terms {
		if term == "на" || term == "по" || term == "to" {
			t.Fatalf("short term %q should be filtered, got %v", term, terms)
		}
	}
	if len(terms) != 1 || terms[0] != "auth" {
		t.Fatalf("terms=%v want [auth]", terms)
	}
}

func TestPathMatchBoostCyrillicStem(t *testing.T) {
	// "авторизация" is 11 runes; stem must be first 4 runes ("авто"), not 4 bytes ("ав").
	terms := searchQueryTerms("авторизация")
	if len(terms) != 1 || terms[0] != "авторизация" {
		t.Fatalf("terms=%v", terms)
	}
	// Path with only the 2-rune byte-prefix must not get a stem boost.
	shortBoost := pathMatchBoost("modules/ав/handler.bsl", terms)
	if shortBoost >= 0.12 {
		t.Fatalf("byte-prefix path should not get stem boost, got %v", shortBoost)
	}
	// Path containing the 4-rune stem should get +0.12.
	stemBoost := pathMatchBoost("modules/авто_модуль/handler.bsl", terms)
	if stemBoost < 0.12 {
		t.Fatalf("expected Cyrillic rune stem boost >= 0.12, got %v", stemBoost)
	}
	// Full term in basename should prefer the BSL path over prose.
	hits := []zvec.SearchHit{
		{Path: "docs/guide.md", Score: 0.31, ChunkStrategy: "prose"},
		{Path: "modules/авторизация.bsl", Score: 0.66, ChunkStrategy: "ast"},
	}
	out := rerankSearchHits(hits, "авторизация пользователя")
	if out[0].Path != "modules/авторизация.bsl" {
		t.Fatalf("first hit path=%q want авторизация.bsl", out[0].Path)
	}
}
