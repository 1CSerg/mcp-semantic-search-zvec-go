package service

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

const (
	searchOverFetchMin = 40
	searchOverFetchMax = 100
	searchOverFetchMul = 5
)

// searchOverFetchLimit returns how many ANN hits to pull before rerank/truncate.
func searchOverFetchLimit(limit int) int {
	if limit <= 0 {
		limit = 10
	}
	n := limit * searchOverFetchMul
	if n < searchOverFetchMin {
		n = searchOverFetchMin
	}
	if n > searchOverFetchMax {
		n = searchOverFetchMax
	}
	return n
}

// rerankSearchHits applies lightweight boosts/penalties so code matching query
// terms ranks above prose/docs and micro-chunks with similar vector scores.
// Lower adjusted score is better (cosine distance).
func rerankSearchHits(hits []zvec.SearchHit, query string) []zvec.SearchHit {
	if len(hits) < 2 {
		return hits
	}
	terms := searchQueryTerms(query)
	type ranked struct {
		hit   zvec.SearchHit
		score float64
	}
	rankedHits := make([]ranked, len(hits))
	for i, h := range hits {
		adj := h.Score
		adj -= pathMatchBoost(h.Path, terms)
		adj -= symbolMatchBoost(h.SymbolName, terms)
		if h.ChunkStrategy == "ast" || h.ChunkStrategy == "partial" {
			adj -= 0.06
		}
		adj += pathDemotePenalty(h.Path)
		adj += microChunkPenalty(h)
		rankedHits[i] = ranked{hit: h, score: adj}
	}
	sort.SliceStable(rankedHits, func(i, j int) bool {
		return rankedHits[i].score < rankedHits[j].score
	})
	out := make([]zvec.SearchHit, len(hits))
	for i, r := range rankedHits {
		out[i] = r.hit
	}
	return out
}

func searchQueryTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var terms []string
	seen := make(map[string]struct{})
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if utf8.RuneCountInString(f) < 3 {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		terms = append(terms, f)
	}
	return terms
}

func pathMatchBoost(path string, terms []string) float64 {
	if path == "" || len(terms) == 0 {
		return 0
	}
	p := strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	var boost float64
	for _, term := range terms {
		if strings.Contains(p, term) {
			boost += 0.18
		}
		if strings.Contains(base, term) {
			boost += 0.22
		}
		rs := []rune(term)
		if len(rs) >= 5 {
			stem := string(rs[:4])
			if strings.Contains(p, stem) {
				boost += 0.12
			}
		}
	}
	return boost
}

func symbolMatchBoost(symbolName string, terms []string) float64 {
	if symbolName == "" || len(terms) == 0 {
		return 0
	}
	sym := strings.ToLower(symbolName)
	var boost float64
	for _, term := range terms {
		if strings.Contains(sym, term) {
			boost += 0.20
		}
	}
	return boost
}

func pathDemotePenalty(path string) float64 {
	if path == "" {
		return 0
	}
	p := strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(filepath.Base(p))
	switch base {
	case "install.md", "agents.md", "readme.md", "changelog.md":
		return 0.12
	}
	if strings.HasPrefix(p, "docs/") || strings.Contains(p, "/docs/") {
		return 0.12
	}
	if strings.Contains(p, "/testdata/") || strings.HasPrefix(p, "testdata/") {
		return 0.10
	}
	if strings.Contains(p, "tests/realworld/corpus/") {
		return 0.10
	}
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".md", ".markdown", ".mdc", ".txt":
		return 0.08
	}
	return 0
}

func microChunkPenalty(h zvec.SearchHit) float64 {
	lines := h.EndLine - h.StartLine
	if lines < 0 {
		lines = 0
	}
	if lines < 2 || len(h.Snippet) < 40 {
		return 0.10
	}
	return 0
}
