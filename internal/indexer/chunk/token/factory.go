package token

import (
	"fmt"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

// NewCounter builds a TokenCounter for the active embedding profile.
func NewCounter(profile config.EmbeddingProfile, workspaceRoot string) (TokenCounter, error) {
	switch strings.ToLower(profile.Provider) {
	case "onnx":
		return newONNXCounter(profile, workspaceRoot)
	default:
		return &HeuristicCounter{}, nil
	}
}

// CharCounter counts one token per rune (deterministic for tests).
type CharCounter struct{}

func (CharCounter) Count(text string) int {
	return len([]rune(text))
}

// FixedCounter always returns a fixed count (for tests).
type FixedCounter struct {
	N int
}

func (f FixedCounter) Count(string) int {
	if f.N <= 0 {
		return 1
	}
	return f.N
}

// BudgetBodyTokens computes body token budget after optional context prefix.
func BudgetBodyTokens(maxInput int, ratio float64, counter TokenCounter, relativePath, parentScope string, contextPrefix bool) int {
	total := int(float64(maxInput) * ratio)
	if total <= 0 {
		total = maxInput
	}
	if !contextPrefix || counter == nil {
		return total
	}
	prefix := formatContextPrefix(relativePath, parentScope)
	remain := total - counter.Count(prefix)
	if remain < 1 {
		return 1
	}
	return remain
}

func formatContextPrefix(relativePath, parentScope string) string {
	rel := strings.ReplaceAll(relativePath, "\\", "/")
	if parentScope == "" {
		return "// file: " + rel + "\n"
	}
	return fmt.Sprintf("// file: %s\n// scope: %s\n", rel, parentScope)
}
