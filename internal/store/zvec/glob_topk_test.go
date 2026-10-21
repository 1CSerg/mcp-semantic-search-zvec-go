package zvec

import "testing"

func TestGlobTopKStart(t *testing.T) {
	tests := []struct {
		name           string
		topK, docCount int
		want           int
	}{
		{"empty collection", 5, 0, 0},
		{"negative docCount", 5, -3, 0},
		{"topK below docCount", 1, 121, 1},
		{"topK equals docCount", 7, 7, 7},
		{"topK above docCount", 5, 2, 2},
		{"topK far above docCount", 1000, 3, 3},
		{"non-positive topK clamps to 1", 0, 9, 1},
		{"negative topK clamps to 1", -4, 9, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := globTopKStart(tc.topK, tc.docCount); got != tc.want {
				t.Fatalf("globTopKStart(%d, %d) = %d want %d", tc.topK, tc.docCount, got, tc.want)
			}
		})
	}
}

func TestNextGlobTopK(t *testing.T) {
	const maxInt = int(^uint(0) >> 1)
	tests := []struct {
		name         string
		current, doc int
		want         int
	}{
		{"empty collection", 1, 0, 0},
		{"negative docCount", 1, -1, 0},
		{"doubles below docCount", 1, 121, 2},
		{"doubles 32", 32, 121, 64},
		// The regression: 64*2 = 128 overshoots docCount=121, so the next window
		// must be the full collection instead of terminating the loop.
		{"caps at docCount between 64 and 128", 64, 121, 121},
		{"caps at docCount between 64 and 128 (exact 100)", 64, 100, 100},
		{"exact power of two below docCount", 32, 64, 64},
		{"current equals docCount", 121, 121, 121},
		{"current above docCount", 200, 121, 121},
		{"doubling lands exactly on docCount", 64, 128, 128},
		{"overflow guard", maxInt/2 + 1, maxInt, maxInt},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextGlobTopK(tc.current, tc.doc); got != tc.want {
				t.Fatalf("nextGlobTopK(%d, %d) = %d want %d", tc.current, tc.doc, got, tc.want)
			}
		})
	}
}

// TestGlobExpansionScheduleTerminates replays the expansion schedule for the
// shapes that used to return zero hits and asserts the loop reaches a full pass
// of the collection.
func TestGlobExpansionScheduleTerminates(t *testing.T) {
	tests := []struct {
		name     string
		topK     int
		docCount int
	}{
		{"integration shape 120 other + 1 target", 1, 121},
		{"topK above docCount", 5, 2},
		{"single doc", 10, 1},
		{"large collection", 10, 1000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			current := globTopKStart(tc.topK, tc.docCount)
			if current < 1 || current > tc.docCount {
				t.Fatalf("start %d outside (0, %d]", current, tc.docCount)
			}
			steps := 0
			for current < tc.docCount {
				next := nextGlobTopK(current, tc.docCount)
				if next <= current {
					t.Fatalf("schedule did not advance: %d -> %d", current, next)
				}
				if next > tc.docCount {
					t.Fatalf("schedule exceeded docCount: %d -> %d (docCount %d)", current, next, tc.docCount)
				}
				current = next
				steps++
				if steps > 64 {
					t.Fatalf("schedule did not terminate after %d steps", steps)
				}
			}
			if current != tc.docCount {
				t.Fatalf("final window %d want full pass %d", current, tc.docCount)
			}
		})
	}
}
