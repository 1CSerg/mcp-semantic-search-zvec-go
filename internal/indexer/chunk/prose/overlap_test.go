package prose

import (
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
)

func TestProseOverlap_WordIntegrity(t *testing.T) {
	counter := token.CharCounter{}
	prev := "The quick brown fox jumps over the lazy dog near the river bank."
	cfg := Config{ProseOverlapRatio: 0.15, MaxInputTokens: 120, EmbedBudgetRatio: 1.0}
	ov := overlapSuffix(prev, cfg.overlapTokens(cfg.MaxInputTokens), counter)
	if ov == "" {
		t.Fatal("expected overlap suffix")
	}
	if !overlapStartsAtBoundary(prev, ov) {
		t.Fatalf("overlap not at word boundary: %q in %q", ov, prev)
	}
	if idx := strings.Index(prev, ov); idx > 0 && !isWordBoundaryAt(prev, idx) {
		t.Fatalf("overlap starts mid-word at %d: %q", idx, ov)
	}
	prevRU := "Быстрая коричневая лиса прыгает через ленивую собаку у реки."
	ovRU := overlapSuffix(prevRU, cfg.overlapTokens(100), counter)
	if ovRU == "" {
		t.Fatal("expected cyrillic overlap")
	}
	if !overlapStartsAtBoundary(prevRU, ovRU) {
		t.Fatalf("cyrillic overlap not at boundary: %q", ovRU)
	}
}

func isWordBoundaryAt(s string, idx int) bool {
	if idx <= 0 || idx >= len(s) {
		return true
	}
	return s[idx-1] == ' ' || s[idx-1] == '\n' || s[idx-1] == '\t'
}
