package token

import "testing"

func TestHeuristicCounterDefaultRatio(t *testing.T) {
	c := HeuristicCounter{}
	if got := c.Count("abcdefgh"); got != 2 {
		t.Fatalf("Count=%d want 2 for 8 runes / 3.5", got)
	}
}

func TestHeuristicCounterCustomRatio(t *testing.T) {
	c := HeuristicCounter{CharsPerToken: 2}
	if got := c.Count("abcd"); got != 2 {
		t.Fatalf("Count=%d want 2", got)
	}
}

func TestHeuristicCounterEmptyText(t *testing.T) {
	c := HeuristicCounter{}
	if got := c.Count(""); got != 0 {
		t.Fatalf("Count=%d want 0", got)
	}
}

func TestHeuristicCounterZeroRatioUsesDefault(t *testing.T) {
	c := HeuristicCounter{CharsPerToken: 0}
	if got := c.Count("abcdefghij"); got != 2 {
		t.Fatalf("Count=%d want 2 for 10 runes / 3.5", got)
	}
}

func TestHeuristicCounterNegativeRatioUsesDefault(t *testing.T) {
	c := HeuristicCounter{CharsPerToken: -1}
	if got := c.Count("abcdefgh"); got != 2 {
		t.Fatalf("Count=%d want 2 for 8 runes / 3.5", got)
	}
}

func TestHeuristicCounterCyrillicRunes(t *testing.T) {
	c := HeuristicCounter{}
	if got := c.Count("привет"); got != 1 {
		t.Fatalf("Count=%d want 1 for 6 runes / 3.5", got)
	}
}
