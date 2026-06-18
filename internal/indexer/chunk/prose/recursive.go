package prose

import (
	"strings"
	"unicode"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
)

type splitLevel int

const (
	levelParagraph splitLevel = iota
	levelLine
	levelSentence
	levelClause
	levelWord
	levelChar
)

// TextPiece is a split fragment with byte offsets in the original source text.
type TextPiece struct {
	Text  string
	Start int
	End   int
}

// RecursiveSplit breaks text into pieces each within budget using delimiter hierarchy.
// Each piece carries Start/End byte offsets in the trimmed source text.
func RecursiveSplit(text string, budget int, counter token.TokenCounter) []TextPiece {
	if counter == nil {
		counter = &token.HeuristicCounter{}
	}
	trimmed, lead, _ := trimSpaceBounds(text)
	if trimmed == "" {
		return nil
	}
	if counter.Count(trimmed) <= budget {
		return []TextPiece{{Text: trimmed, Start: lead, End: lead + len(trimmed)}}
	}
	return recursiveSplit(trimmed, lead, budget, counter, levelParagraph)
}

func recursiveSplit(text string, baseOffset int, budget int, counter token.TokenCounter, level splitLevel) []TextPiece {
	trimmed, lead, _ := trimSpaceBounds(text)
	if trimmed == "" {
		return nil
	}
	baseOffset += lead
	text = trimmed
	if counter.Count(text) <= budget {
		return []TextPiece{{Text: text, Start: baseOffset, End: baseOffset + len(text)}}
	}
	if level >= levelChar {
		return splitByCharsWithSpans(text, baseOffset, budget, counter)
	}
	parts := splitByLevelWithSpans(text, baseOffset, level)
	if len(parts) <= 1 {
		return recursiveSplit(text, baseOffset, budget, counter, level+1)
	}
	var out []TextPiece
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		sub := recursiveSplit(p.Text, p.Start, budget, counter, level+1)
		if len(sub) == 0 {
			continue
		}
		out = append(out, mergeGreedyPieces(sub, separatorForLevel(level), budget, counter)...)
	}
	if len(out) == 0 {
		return recursiveSplit(text, baseOffset, budget, counter, level+1)
	}
	return mergeGreedyPieces(out, separatorForLevel(level), budget, counter)
}

func separatorForLevel(level splitLevel) string {
	switch level {
	case levelParagraph:
		return "\n\n"
	case levelLine:
		return "\n"
	case levelSentence, levelClause, levelWord:
		return " "
	default:
		return ""
	}
}

func splitByLevelWithSpans(text string, baseOffset int, level splitLevel) []TextPiece {
	switch level {
	case levelParagraph:
		return splitParagraphsWithSpans(text, baseOffset)
	case levelLine:
		return splitLinesWithSpans(text, baseOffset)
	case levelSentence:
		return splitSentencesWithSpans(text, baseOffset)
	case levelClause:
		return splitClausesWithSpans(text, baseOffset)
	case levelWord:
		return splitWordsWithSpans(text, baseOffset)
	default:
		return []TextPiece{{Text: text, Start: baseOffset, End: baseOffset + len(text)}}
	}
}

func trimSpaceBounds(s string) (trimmed string, lead, trail int) {
	trimmed = strings.TrimSpace(s)
	if trimmed == "" {
		return "", 0, len(s)
	}
	lead = strings.Index(s, trimmed)
	trail = len(s) - lead - len(trimmed)
	return trimmed, lead, trail
}

func splitParagraphsWithSpans(text string, baseOffset int) []TextPiece {
	var parts []TextPiece
	start := 0
	for i := 0; i < len(text); {
		if i+1 < len(text) && text[i] == '\n' {
			j := i + 1
			for j < len(text) && text[j] == '\n' {
				j++
			}
			if j > i+1 {
				if p := pieceFromRawSpan(text, baseOffset, start, i); p.Text != "" {
					parts = append(parts, p)
				}
				start = j
				i = j
				continue
			}
		}
		i++
	}
	if p := pieceFromRawSpan(text, baseOffset, start, len(text)); p.Text != "" {
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return []TextPiece{{Text: strings.TrimSpace(text), Start: baseOffset, End: baseOffset + len(text)}}
	}
	return parts
}

func pieceFromRawSpan(text string, baseOffset, rawStart, rawEnd int) TextPiece {
	raw := text[rawStart:rawEnd]
	trimmed, lead, _ := trimSpaceBounds(raw)
	if trimmed == "" {
		return TextPiece{}
	}
	start := baseOffset + rawStart + lead
	return TextPiece{Text: trimmed, Start: start, End: start + len(trimmed)}
}

func splitLinesWithSpans(text string, baseOffset int) []TextPiece {
	var out []TextPiece
	lineStart := 0
	for i := 0; i <= len(text); i++ {
		if i < len(text) && text[i] != '\n' {
			continue
		}
		if p := pieceFromRawSpan(text, baseOffset, lineStart, i); p.Text != "" {
			out = append(out, p)
		}
		lineStart = i + 1
	}
	if len(out) == 0 {
		return []TextPiece{{Text: strings.TrimSpace(text), Start: baseOffset, End: baseOffset + len(text)}}
	}
	return out
}

func splitSentencesWithSpans(text string, baseOffset int) []TextPiece {
	runes := []rune(text)
	var parts []TextPiece
	start := 0
	for i := 0; i < len(runes); i++ {
		if !isSentenceEnd(runes[i]) {
			continue
		}
		j := i + 1
		for j < len(runes) && unicode.IsSpace(runes[j]) {
			j++
		}
		if j >= len(runes) {
			if p := pieceFromRawSpan(text, baseOffset, byteOffset(text, start), len(text)); p.Text != "" {
				parts = append(parts, p)
			}
			return parts
		}
		if unicode.IsUpper(runes[j]) || unicode.IsDigit(runes[j]) {
			endByte := byteOffset(text, j)
			if p := pieceFromRawSpan(text, baseOffset, byteOffset(text, start), endByte); p.Text != "" {
				parts = append(parts, p)
			}
			start = j
			i = j - 1
		}
	}
	if p := pieceFromRawSpan(text, baseOffset, byteOffset(text, start), len(text)); p.Text != "" {
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return []TextPiece{{Text: strings.TrimSpace(text), Start: baseOffset, End: baseOffset + len(text)}}
	}
	return parts
}

func byteOffset(text string, runeIndex int) int {
	return len(string([]rune(text)[:runeIndex]))
}

func isSentenceEnd(r rune) bool {
	switch r {
	case '.', '!', '?', '…':
		return true
	default:
		return false
	}
}

func splitClausesWithSpans(text string, baseOffset int) []TextPiece {
	var parts []TextPiece
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] != ';' && text[i] != ':' {
			continue
		}
		j := i + 1
		if j >= len(text) || !unicode.IsSpace(rune(text[j])) {
			continue
		}
		for j < len(text) && unicode.IsSpace(rune(text[j])) {
			j++
		}
		if p := pieceFromRawSpan(text, baseOffset, start, j); p.Text != "" {
			parts = append(parts, p)
		}
		start = j
		i = j - 1
	}
	if p := pieceFromRawSpan(text, baseOffset, start, len(text)); p.Text != "" {
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return []TextPiece{{Text: strings.TrimSpace(text), Start: baseOffset, End: baseOffset + len(text)}}
	}
	return parts
}

func splitWordsWithSpans(text string, baseOffset int) []TextPiece {
	var out []TextPiece
	i := 0
	for i < len(text) {
		for i < len(text) && unicode.IsSpace(rune(text[i])) {
			i++
		}
		if i >= len(text) {
			break
		}
		wordStart := i
		for i < len(text) && !unicode.IsSpace(rune(text[i])) {
			i++
		}
		out = append(out, TextPiece{
			Text:  text[wordStart:i],
			Start: baseOffset + wordStart,
			End:   baseOffset + i,
		})
	}
	if len(out) == 0 {
		return []TextPiece{{Text: strings.TrimSpace(text), Start: baseOffset, End: baseOffset + len(text)}}
	}
	return out
}

func splitByCharsWithSpans(text string, baseOffset int, budget int, counter token.TokenCounter) []TextPiece {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	var out []TextPiece
	for start := 0; start < len(runes); {
		end := start + 1
		for end <= len(runes) && counter.Count(string(runes[start:end])) <= budget {
			end++
		}
		if end-1 == start {
			pieceText := string(runes[start:end])
			byteStart := baseOffset + len(string(runes[:start]))
			out = append(out, TextPiece{Text: pieceText, Start: byteStart, End: byteStart + len(pieceText)})
			start = end
			continue
		}
		pieceText := string(runes[start : end-1])
		byteStart := baseOffset + len(string(runes[:start]))
		out = append(out, TextPiece{Text: pieceText, Start: byteStart, End: byteStart + len(pieceText)})
		start = end - 1
	}
	return out
}

// mergeGreedyPieces combines adjacent pieces while total token count stays within budget.
// Merged spans cover the full source range from the first piece start through the last piece end.
func mergeGreedyPieces(parts []TextPiece, separator string, budget int, counter token.TokenCounter) []TextPiece {
	if len(parts) == 0 {
		return nil
	}
	var merged []TextPiece
	cur := parts[0]
	for i := 1; i < len(parts); i++ {
		candidate := cur.Text + separator + parts[i].Text
		if counter.Count(candidate) <= budget {
			cur = TextPiece{
				Text:  candidate,
				Start: cur.Start,
				End:   parts[i].End,
			}
			continue
		}
		merged = append(merged, cur)
		cur = parts[i]
	}
	merged = append(merged, cur)
	return merged
}

// mergeGreedy combines adjacent string pieces (tests only).
func mergeGreedy(parts []string, separator string, budget int, counter token.TokenCounter) []string {
	pieces := make([]TextPiece, len(parts))
	for i, p := range parts {
		pieces[i] = TextPiece{Text: p, Start: 0, End: len(p)}
	}
	merged := mergeGreedyPieces(pieces, separator, budget, counter)
	out := make([]string, len(merged))
	for i, p := range merged {
		out[i] = p.Text
	}
	return out
}

// splitParagraphs splits on blank lines (tests).
func splitParagraphs(text string) []string {
	return piecesToStrings(splitParagraphsWithSpans(text, 0))
}

// splitClauses splits on ; or : followed by whitespace (tests).
func splitClauses(text string) []string {
	return piecesToStrings(splitClausesWithSpans(text, 0))
}

// splitSentences splits on sentence boundaries (tests).
func splitSentences(text string) []string {
	return piecesToStrings(splitSentencesWithSpans(text, 0))
}

// splitByChars splits rune-wise to fit budget (tests).
func splitByChars(text string, budget int, counter token.TokenCounter) []string {
	return piecesToStrings(splitByCharsWithSpans(text, 0, budget, counter))
}

func piecesToStrings(parts []TextPiece) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = p.Text
	}
	return out
}

// overlapSuffix returns a suffix of prev with at most overlapTokens, trimmed at word boundary.
func overlapSuffix(prev string, overlapTokens int, counter token.TokenCounter) string {
	if overlapTokens <= 0 || prev == "" {
		return ""
	}
	runes := []rune(prev)
	for start := 0; start < len(runes); start++ {
		suffix := string(runes[start:])
		if counter.Count(suffix) <= overlapTokens {
			return trimOverlapStart(suffix)
		}
	}
	return trimOverlapStart(prev)
}

func trimOverlapStart(s string) string {
	s = strings.TrimLeft(s, " \t\n")
	if s == "" {
		return ""
	}
	if cut := preferredOverlapCut(s); cut > 0 {
		s = strings.TrimLeft(s[cut:], " \t\n")
	}
	return s
}

// preferredOverlapCut returns a byte offset to trim when overlap starts mid-unit.
// Sentence boundaries are preferred over word boundaries.
func preferredOverlapCut(s string) int {
	if startsAtNaturalBoundary(s) {
		return 0
	}
	if cut := firstSentenceStartIn(s); cut > 0 {
		return cut
	}
	if idx := strings.IndexFunc(s, unicode.IsSpace); idx > 0 {
		return idx
	}
	return 0
}

func startsAtNaturalBoundary(s string) bool {
	runes := []rune(strings.TrimLeft(s, " \t\n"))
	if len(runes) == 0 {
		return true
	}
	r := runes[0]
	if unicode.IsUpper(r) || unicode.IsDigit(r) {
		return true
	}
	if r == '#' || r == '-' || r == '|' || r == '>' {
		return true
	}
	return false
}

func firstSentenceStartIn(s string) int {
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if !isSentenceEnd(runes[i]) {
			continue
		}
		j := i + 1
		for j < len(runes) && unicode.IsSpace(runes[j]) {
			j++
		}
		if j < len(runes) {
			return len(string(runes[:j]))
		}
	}
	return 0
}

// overlapStartsAtBoundary reports whether suffix begins at a word or sentence boundary in full.
func overlapStartsAtBoundary(full, suffix string) bool {
	suffix = strings.TrimLeft(suffix, " \t\n")
	if suffix == "" {
		return true
	}
	idx := strings.Index(full, suffix)
	if idx < 0 {
		return true
	}
	if idx == 0 {
		return startsAtNaturalBoundary(suffix)
	}
	before := full[:idx]
	if strings.HasSuffix(before, " ") || strings.HasSuffix(before, "\t") || strings.HasSuffix(before, "\n") {
		return true
	}
	for i := len(before) - 1; i >= 0; i-- {
		if isSentenceEnd(rune(before[i])) {
			return true
		}
		if before[i] == ' ' || before[i] == '\t' || before[i] == '\n' {
			break
		}
	}
	return false
}

// deepestSplitLevel reports the coarsest level needed to fit text in budget.
func deepestSplitLevel(text string, budget int, counter token.TokenCounter) string {
	if counter.Count(text) <= budget {
		return "paragraph"
	}
	for level := levelParagraph; level < levelChar; level++ {
		parts := splitByLevelWithSpans(text, 0, level)
		if len(parts) > 1 {
			allFit := true
			for _, p := range parts {
				if counter.Count(p.Text) > budget {
					allFit = false
					break
				}
			}
			if allFit {
				switch level {
				case levelParagraph:
					return "paragraph"
				case levelLine:
					return "paragraph"
				case levelSentence:
					return "sentence"
				case levelClause, levelWord:
					return "sentence"
				}
			}
		}
	}
	return "sentence"
}
