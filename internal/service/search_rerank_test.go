package service

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func TestSearchOverFetchLimit(t *testing.T) {
	if got := searchOverFetchLimit(10); got != 50 {
		t.Fatalf("limit 10: got %d want 50", got)
	}
	if got := searchOverFetchLimit(5); got != 40 {
		t.Fatalf("limit 5: got %d want 40 (min)", got)
	}
	if got := searchOverFetchLimit(30); got != 100 {
		t.Fatalf("limit 30: got %d want 100 (max)", got)
	}
}

func TestRerankSearchHitsPrefersPathMatch(t *testing.T) {
	hits := []zvec.SearchHit{
		{Path: "docs/guide.md", Score: 0.31, ChunkStrategy: "prose", Snippet: "authentication middleware documentation section"},
		{Path: "backend/auth/middleware.go", Score: 0.66, ChunkStrategy: "ast", StartLine: 10, EndLine: 40, Snippet: "func AuthMiddleware(next http.Handler) http.Handler {\n\treturn next\n}"},
	}
	out := rerankSearchHits(hits, "authentication middleware gate")
	if len(out) < 2 {
		t.Fatal("expected reranked hits")
	}
	if out[0].Path != "backend/auth/middleware.go" {
		t.Fatalf("first hit path=%q want middleware.go", out[0].Path)
	}
}

func TestRerankSearchHitsPreservesOrderWhenEqual(t *testing.T) {
	hits := []zvec.SearchHit{
		{Path: "a.go", Score: 0.1, ChunkStrategy: "ast", StartLine: 1, EndLine: 20, Snippet: "func A() { /* longer than forty characters here */ }"},
		{Path: "b.go", Score: 0.2, ChunkStrategy: "ast", StartLine: 1, EndLine: 20, Snippet: "func B() { /* longer than forty characters here */ }"},
	}
	out := rerankSearchHits(hits, "  ")
	if out[0].Path != "a.go" || out[1].Path != "b.go" {
		t.Fatalf("unexpected reorder: %+v", out)
	}
}

func TestRerankSearchHitsDemotesDocsOverCode(t *testing.T) {
	hits := []zvec.SearchHit{
		{Path: "docs/ARCHITECTURE.md", Score: 0.30, ChunkStrategy: "prose", Snippet: "WorkspaceRegistry borrow release lifecycle section text"},
		{Path: "internal/daemon/registry.go", Score: 0.32, ChunkStrategy: "ast", SymbolName: "Registry", StartLine: 50, EndLine: 80, Snippet: "type Registry struct {\n\topen map[string]*workspaceHandle\n}"},
	}
	out := rerankSearchHits(hits, "workspace registry borrow release")
	if out[0].Path != "internal/daemon/registry.go" {
		t.Fatalf("first hit path=%q want registry.go", out[0].Path)
	}
}

func TestRerankSearchHitsDemotesMicroChunk(t *testing.T) {
	hits := []zvec.SearchHit{
		{Path: "internal/pkg/const.go", Score: 0.30, ChunkStrategy: "ast", SymbolName: "tsxLanguage", StartLine: 26, EndLine: 26, Snippet: "tsxLanguage"},
		{Path: "internal/indexer/chunk/router.go", Score: 0.31, ChunkStrategy: "ast", SymbolName: "ChunkRouter", StartLine: 29, EndLine: 60, Snippet: "// ChunkRouter selects AST or line_window chunking per file extension.\ntype ChunkRouter struct{}"},
	}
	out := rerankSearchHits(hits, "ChunkRouter tree-sitter hybrid")
	if out[0].Path != "internal/indexer/chunk/router.go" {
		t.Fatalf("first hit path=%q want router.go", out[0].Path)
	}
}

func TestRerankSearchHitsSymbolBoost(t *testing.T) {
	hits := []zvec.SearchHit{
		{Path: "internal/other/helper.go", Score: 0.30, ChunkStrategy: "ast", SymbolName: "Helper", StartLine: 1, EndLine: 20, Snippet: "func Helper() { /* longer than forty characters of body */ }"},
		{Path: "internal/daemon/registry.go", Score: 0.31, ChunkStrategy: "ast", SymbolName: "BorrowService", StartLine: 100, EndLine: 130, Snippet: "func (r *Registry) BorrowService(id string) (*Phase1, func(), error) {\n\treturn nil, nil, nil\n}"},
	}
	out := rerankSearchHits(hits, "BorrowService discardHandle")
	if out[0].SymbolName != "BorrowService" {
		t.Fatalf("first symbol=%q want BorrowService", out[0].SymbolName)
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
		{Path: "docs/guide.md", Score: 0.31, ChunkStrategy: "prose", Snippet: "описание авторизация пользователя в документации"},
		{Path: "modules/авторизация.bsl", Score: 0.66, ChunkStrategy: "ast", StartLine: 1, EndLine: 30, Snippet: "Процедура АвторизацияПользователя()\nКонецПроцедуры"},
	}
	out := rerankSearchHits(hits, "авторизация пользователя")
	if out[0].Path != "modules/авторизация.bsl" {
		t.Fatalf("first hit path=%q want авторизация.bsl", out[0].Path)
	}
}
