package zvec

// globTopKStart returns the first queryTopK used by the path_glob expansion loop.
// The value is clamped to the collection size so the loop always runs at least
// once even when the caller asked for more hits than the collection holds.
func globTopKStart(topK, docCount int) int {
	if docCount <= 0 {
		return 0
	}
	if topK < 1 {
		topK = 1
	}
	if topK > docCount {
		return docCount
	}
	return topK
}

// nextGlobTopK returns the next queryTopK for the path_glob expansion loop.
// It doubles the window but never exceeds docCount, so the final iteration is a
// full pass over the collection: the Go-side glob filter then sees every
// document instead of stopping after an overshooting doubling step.
// A non-positive result means there is no further step (docCount <= 0).
func nextGlobTopK(current, docCount int) int {
	if docCount <= 0 {
		return 0
	}
	if current >= docCount {
		return docCount
	}
	next := current * 2
	if next <= current || next > docCount {
		next = docCount
	}
	return next
}
