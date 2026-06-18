package token

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestNewCounterOpenAI(t *testing.T) {
	c, err := NewCounter(config.EmbeddingProfile{Provider: "openai_compatible"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.(*HeuristicCounter); !ok {
		t.Fatalf("type=%T", c)
	}
}

func TestCharCounter(t *testing.T) {
	c := CharCounter{}
	if got := c.Count("abc"); got != 3 {
		t.Fatalf("Count=%d", got)
	}
}

func TestFixedCounter(t *testing.T) {
	if got := (FixedCounter{N: 7}).Count("anything"); got != 7 {
		t.Fatalf("Count=%d", got)
	}
	if got := (FixedCounter{N: 0}).Count("x"); got != 1 {
		t.Fatalf("Count=%d", got)
	}
}

func TestBudgetBodyTokens(t *testing.T) {
	counter := CharCounter{}
	if got := BudgetBodyTokens(100, 0.9, counter, "a.go", "", false); got != 90 {
		t.Fatalf("got=%d", got)
	}
	withPrefix := BudgetBodyTokens(100, 1.0, counter, "pkg/a.go", "main", true)
	prefix := formatContextPrefix("pkg/a.go", "main")
	if withPrefix >= 100 {
		t.Fatalf("body budget should shrink for prefix: %d", withPrefix)
	}
	if counter.Count(prefix)+withPrefix > 100 {
		t.Fatalf("prefix+budget exceeds max")
	}
}

func TestFormatContextPrefix(t *testing.T) {
	if got := formatContextPrefix(`pkg\a.go`, ""); got != "// file: pkg/a.go\n" {
		t.Fatalf("got=%q", got)
	}
	if got := formatContextPrefix("a.go", "main"); got == "" {
		t.Fatal("expected scope prefix")
	}
}
