package ast

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
)

func TestConfigBodyBudgetContext(t *testing.T) {
	cfg := Config{ContextPrefix: true, MaxInputTokens: 100, EmbedBudgetRatio: 0.9}
	counter := token.CharCounter{}
	b := cfg.bodyBudget(counter, "f.py", "module x")
	if b <= 0 {
		t.Fatalf("budget=%d", b)
	}
}
