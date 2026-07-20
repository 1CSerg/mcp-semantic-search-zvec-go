package service

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

// rerankSearchHits applies lightweight path-token boosts so code paths matching
// query terms (e.g. "middleware" → middleware.go) rank above prose with similar text.
func rerankSearchHits(hits []zvec.SearchHit, query string) []zvec.SearchHit {
	if len(hits) < 2 {
		return hits
	}
	terms := searchQueryTerms(query)
	if len(terms) == 0 {
		return hits
	}
	type ranked struct {
		hit   zvec.SearchHit
		score float64
	}
	rankedHits := make([]ranked, len(hits))
	for i, h := range hits {
		adj := h.Score - pathMatchBoost(h.Path, terms)
		if h.ChunkStrategy == "ast" || h.ChunkStrategy == "partial" {
			adj -= 0.04
		}
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
