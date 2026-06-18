package token

// TokenCounter abstracts the tokenization logic to verify chunk sizes against budgets.
type TokenCounter interface {
	// Count returns the number of tokens in the given text.
	Count(text string) int
}

// HeuristicCounter is a fallback counter based on rune/byte length.
type HeuristicCounter struct {
	// CharsPerToken — делитель длины текста; меньше = консервативнее (safety margin).
	// Default 3.5 (~4 chars/token с запасом для кириллицы и коротких токенов).
	CharsPerToken float64
}

// Count implements TokenCounter using a simple heuristic.
func (c *HeuristicCounter) Count(text string) int {
	ratio := c.CharsPerToken
	if ratio <= 0 {
		ratio = 3.5
	}
	return int(float64(len([]rune(text))) / ratio)
}
