package prose

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

type segmentKind int

const (
	segProse segmentKind = iota
	segCodeBlock
	segFrontMatter
	segTable
)

type markdownSegment struct {
	kind          segmentKind
	text          string
	startLine     int64
	endLine       int64
	symbolName    string
	symbolKind    string
	parentScope   string
	noOverlapNext bool
}

type mdState struct {
	inCodeBlock     bool
	fenceMarker     byte
	fenceLen        int
	inTable         bool
	inFrontMatter   bool
	frontMatterDone bool
	headingLevel    int
	headingStack    []string
	currentLines    []string
	currentStart    int64
	lineNum         int64
	pendingHeading  string
}

func newMDState() *mdState {
	return &mdState{lineNum: 0}
}

// parseMarkdownSegments splits markdown into logical segments using a line state machine.
func parseMarkdownSegments(content string) []markdownSegment {
	lines := strings.Split(content, "\n")
	st := newMDState()
	var segments []markdownSegment
	for _, line := range lines {
		segs := st.feedLine(line)
		segments = append(segments, segs...)
	}
	segments = append(segments, st.flushAll()...)
	return segments
}

func (st *mdState) feedLine(line string) []markdownSegment {
	st.lineNum++
	var out []markdownSegment
	if st.inFrontMatter {
		st.currentLines = append(st.currentLines, line)
		if strings.TrimSpace(line) == "---" && len(st.currentLines) > 1 {
			out = append(out, st.emitCurrent(segFrontMatter, "", "section", st.parentScope(), false)...)
			st.inFrontMatter = false
			st.frontMatterDone = true
		}
		return out
	}
	if !st.frontMatterDone && st.lineNum == 1 && strings.TrimSpace(line) == "---" {
		st.inFrontMatter = true
		st.currentStart = st.lineNum
		st.currentLines = []string{line}
		return nil
	}
	if st.inCodeBlock {
		if isFenceClose(line, st.fenceMarker, st.fenceLen) {
			st.currentLines = append(st.currentLines, line)
			out = append(out, st.emitCurrent(segCodeBlock, "", "code_block", st.parentScope(), false)...)
			st.inCodeBlock = false
			st.fenceMarker = 0
			st.fenceLen = 0
			return out
		}
		st.currentLines = append(st.currentLines, line)
		return nil
	}
	if marker, length, ok := parseFenceOpen(line); ok {
		out = append(out, st.flushProse(false)...)
		st.inCodeBlock = true
		st.fenceMarker = marker
		st.fenceLen = length
		st.currentStart = st.lineNum
		st.currentLines = []string{line}
		return out
	}
	if level, title, ok := parseHeading(line); ok {
		out = append(out, st.flushProse(true)...)
		st.headingLevel = level
		st.headingStack = updateHeadingStack(st.headingStack, level, title)
		st.currentStart = st.lineNum
		st.currentLines = []string{line}
		st.pendingHeading = title
		return out
	}
	if isTableRow(line) {
		if !st.inTable {
			out = append(out, st.flushProse(false)...)
			st.inTable = true
			st.currentStart = st.lineNum
			st.currentLines = []string{line}
			return out
		}
		st.currentLines = append(st.currentLines, line)
		return nil
	}
	if st.inTable {
		out = append(out, st.emitCurrent(segTable, "", "paragraph", st.parentScope(), false)...)
		st.inTable = false
	}
	if len(st.currentLines) == 0 {
		st.currentStart = st.lineNum
	}
	if strings.TrimSpace(line) == "" && len(st.currentLines) == 0 {
		return out
	}
	st.currentLines = append(st.currentLines, line)
	return out
}

func (st *mdState) flushAll() []markdownSegment {
	var out []markdownSegment
	if st.inCodeBlock {
		out = append(out, st.emitCurrent(segCodeBlock, "", "code_block", st.parentScope(), false)...)
		st.inCodeBlock = false
	} else if st.inFrontMatter {
		out = append(out, st.emitCurrent(segFrontMatter, "", "section", st.parentScope(), false)...)
		st.inFrontMatter = false
	} else if st.inTable {
		out = append(out, st.emitCurrent(segTable, "", "paragraph", st.parentScope(), false)...)
		st.inTable = false
	} else {
		out = append(out, st.flushProse(false)...)
	}
	return out
}

func (st *mdState) parentScope() string {
	if len(st.headingStack) == 0 {
		return ""
	}
	return "section " + strings.Join(st.headingStack, " > ")
}

func (st *mdState) flushProse(noOverlapNext bool) []markdownSegment {
	if len(st.currentLines) == 0 {
		return nil
	}
	symbolName := ""
	symbolKind := "paragraph"
	if st.headingLevel > 0 && strings.HasPrefix(strings.TrimSpace(st.currentLines[0]), "#") {
		_, title, _ := parseHeading(st.currentLines[0])
		symbolName = title
		symbolKind = "section"
		st.pendingHeading = ""
	} else if st.pendingHeading != "" {
		symbolName = st.pendingHeading
		symbolKind = "section"
		st.pendingHeading = ""
	}
	return st.emitCurrent(segProse, symbolName, symbolKind, st.parentScope(), noOverlapNext)
}

func (st *mdState) emitCurrent(kind segmentKind, symbolName, symbolKind, parentScope string, noOverlapNext bool) []markdownSegment {
	if len(st.currentLines) == 0 {
		return nil
	}
	text := strings.Join(st.currentLines, "\n")
	if strings.TrimSpace(text) == "" {
		st.currentLines = nil
		st.headingLevel = 0
		return nil
	}
	if symbolName == "" && st.pendingHeading != "" && kind == segProse {
		symbolName = st.pendingHeading
		st.pendingHeading = ""
	}
	seg := markdownSegment{
		kind:          kind,
		text:          text,
		startLine:     st.currentStart,
		endLine:       st.lineNum,
		symbolName:    symbolName,
		symbolKind:    symbolKind,
		parentScope:   parentScope,
		noOverlapNext: noOverlapNext,
	}
	st.currentLines = nil
	if kind == segProse && symbolKind == "section" {
		st.headingLevel = 0
	}
	return []markdownSegment{seg}
}

func parseHeading(line string) (level int, title string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}
	if i == 0 || i > 6 {
		return 0, "", false
	}
	if i >= len(trimmed) || trimmed[i] != ' ' {
		return 0, "", false
	}
	title = strings.TrimSpace(trimmed[i+1:])
	return i, title, title != ""
}

func updateHeadingStack(stack []string, level int, title string) []string {
	if level <= 0 {
		return stack
	}
	if level > len(stack) {
		return append(stack, title)
	}
	out := append([]string(nil), stack[:level-1]...)
	return append(out, title)
}

func parseFence(line string) (marker string, ok bool) {
	ch, _, ok := parseFenceOpen(line)
	if !ok {
		return "", false
	}
	if ch == '`' {
		return "```", true
	}
	return "~~~", true
}

func parseFenceOpen(line string) (marker byte, length int, ok bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return 0, 0, false
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return 0, 0, false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	return ch, n, true
}

func isFenceClose(line string, openMarker byte, openLen int) bool {
	if openMarker == 0 || openLen < 3 {
		return false
	}
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < openLen {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != openMarker {
			return false
		}
	}
	return len(trimmed) >= openLen
}

func isTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.Contains(trimmed, "|") {
		return false
	}
	return strings.HasPrefix(trimmed, "|") || strings.HasSuffix(trimmed, "|")
}

// ChunkMarkdown splits markdown content and emits zvec chunks.
func ChunkMarkdown(rel string, content string, cfg Config, counter token.TokenCounter, emit EmitFunc) error {
	segments := parseMarkdownSegments(content)
	em := newProseEmitter(rel, cfg, counter, emit)
	for _, seg := range segments {
		if err := em.emitSegment(seg); err != nil {
			return err
		}
	}
	return nil
}

// ChunkPlainText splits plain text and emits zvec chunks.
func ChunkPlainText(rel string, content string, cfg Config, counter token.TokenCounter, emit EmitFunc) error {
	em := newProseEmitter(rel, cfg, counter, emit)
	lines := strings.Count(content, "\n") + 1
	if content == "" {
		lines = 0
	}
	seg := markdownSegment{
		kind:       segProse,
		text:       content,
		startLine:  1,
		endLine:    int64(lines),
		symbolKind: "paragraph",
	}
	return em.emitSegment(seg)
}

type proseEmitter struct {
	rel         string
	cfg         Config
	counter     token.TokenCounter
	emit        EmitFunc
	prevBody    string
	skipOverlap bool
}

func newProseEmitter(rel string, cfg Config, counter token.TokenCounter, emit EmitFunc) *proseEmitter {
	if counter == nil {
		counter = &token.HeuristicCounter{}
	}
	return &proseEmitter{rel: filepath.ToSlash(rel), cfg: cfg, counter: counter, emit: emit}
}

func (em *proseEmitter) emitSegment(seg markdownSegment) error {
	switch seg.kind {
	case segCodeBlock:
		return em.emitCodeBlock(seg)
	case segFrontMatter:
		seg.symbolKind = "section"
		budget := em.cfg.bodyBudget(em.counter, em.rel, seg.parentScope)
		if em.counter.Count(seg.text) > budget {
			return em.emitProseLines(seg)
		}
		return em.emitProse(seg)
	default:
		return em.emitProse(seg)
	}
}

func (em *proseEmitter) emitCodeBlock(seg markdownSegment) error {
	budget := em.cfg.bodyBudget(em.counter, em.rel, seg.parentScope)
	text := seg.text
	if em.counter.Count(text) <= budget {
		return em.emitChunk(text, seg.startLine, seg.endLine, seg.symbolName, "code_block", seg.parentScope, "prose", false)
	}
	lines := strings.Split(text, "\n")
	meta := partialMeta{
		chunkStrategy: "partial",
		symbolKind:    "code_block",
		symbolName:    seg.symbolName,
		parentScope:   seg.parentScope,
	}
	em.prevBody = ""
	em.skipOverlap = true
	return emitPartialWindows(em.rel, lines, seg.startLine, em.cfg, em.counter, meta, em.emit)
}

func (em *proseEmitter) emitProseLines(seg markdownSegment) error {
	lines := strings.Split(seg.text, "\n")
	budget := em.cfg.bodyBudget(em.counter, em.rel, seg.parentScope)
	var buf []string
	var bufStart int64 = seg.startLine
	lineNum := seg.startLine
	for i, ln := range lines {
		candidate := strings.Join(append(buf, ln), "\n")
		if len(buf) > 0 && em.counter.Count(candidate) > budget {
			text := strings.Join(buf, "\n")
			sub := markdownSegment{
				kind: seg.kind, text: text, startLine: bufStart, endLine: lineNum - 1,
				symbolName: seg.symbolName, symbolKind: seg.symbolKind, parentScope: seg.parentScope,
			}
			if err := em.emitProse(sub); err != nil {
				return err
			}
			buf = []string{ln}
			bufStart = seg.startLine + int64(i)
		} else {
			buf = append(buf, ln)
		}
		lineNum++
	}
	if len(buf) > 0 {
		sub := markdownSegment{
			kind: seg.kind, text: strings.Join(buf, "\n"), startLine: bufStart, endLine: seg.endLine,
			symbolName: seg.symbolName, symbolKind: seg.symbolKind, parentScope: seg.parentScope,
		}
		return em.emitProse(sub)
	}
	return nil
}

func (em *proseEmitter) emitProse(seg markdownSegment) error {
	if seg.noOverlapNext {
		em.prevBody = ""
		em.skipOverlap = true
	}
	budget := em.cfg.bodyBudget(em.counter, em.rel, seg.parentScope)
	pieces := RecursiveSplit(seg.text, budget, em.counter)
	if len(pieces) == 0 {
		return nil
	}
	lineStarts := lineOffsets(seg.text)
	for i, piece := range pieces {
		body := piece.Text
		startLine := seg.startLine
		endLine := seg.endLine
		relStart, relEnd := linesForRange(seg.text, lineStarts, piece.Start, piece.End)
		startLine = seg.startLine + relStart - 1
		endLine = seg.startLine + relEnd - 1
		symbolKind := seg.symbolKind
		if symbolKind == "" {
			symbolKind = deepestSplitLevel(body, budget, em.counter)
		}
		if seg.symbolKind == "section" {
			symbolKind = "section"
		} else if symbolKind == "paragraph" && seg.symbolName != "" && i == 0 {
			symbolKind = "section"
		}
		skipOv := em.skipOverlap
		if i > 0 {
			skipOv = false
		}
		if err := em.emitChunk(body, startLine, endLine, seg.symbolName, symbolKind, seg.parentScope, "prose", skipOv); err != nil {
			return err
		}
	}
	if seg.noOverlapNext {
		em.skipOverlap = true
		em.prevBody = ""
	}
	return nil
}

func trimToTokenBudget(text string, budget int, counter token.TokenCounter) string {
	if counter.Count(text) <= budget {
		return text
	}
	runes := []rune(text)
	for start := 1; start < len(runes); start++ {
		s := string(runes[start:])
		if counter.Count(s) <= budget {
			return s
		}
	}
	return text
}

func (em *proseEmitter) emitChunk(body string, startLine, endLine int64, symbolName, symbolKind, parentScope, strategy string, skipOverlap bool) error {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	snippet := body
	if !skipOverlap && em.prevBody != "" {
		bodyBudget := em.cfg.bodyBudget(em.counter, em.rel, parentScope)
		ovBudget := em.cfg.overlapTokens(bodyBudget)
		if ov := overlapSuffix(em.prevBody, ovBudget, em.counter); ov != "" {
			candidate := ov + body
			if em.counter.Count(candidate) <= bodyBudget {
				snippet = candidate
			}
		}
	}
	maxEmbed := em.cfg.MaxInputTokens
	if maxEmbed > 0 && em.counter.Count(snippet) > maxEmbed {
		snippet = trimToTokenBudget(snippet, maxEmbed, em.counter)
	}
	minTok := em.cfg.MinChunkTokens
	if minTok <= 0 {
		minTok = 1
	}
	if em.counter.Count(body) < minTok && symbolKind != "code_block" {
		// still emit small trailing fragments
	}
	ch := &zvec.Chunk{
		DocID:         docID(em.rel, startLine, endLine, symbolName),
		RelativePath:  em.rel,
		StartLine:     startLine,
		EndLine:       endLine,
		ChunkType:     chunkTypeForPath(em.rel),
		Name:          filepath.Base(em.rel),
		Snippet:       snippet,
		SymbolName:    symbolName,
		SymbolKind:    symbolKind,
		ParentScope:   parentScope,
		ChunkStrategy: strategy,
	}
	em.prevBody = body
	em.skipOverlap = false
	return em.emit(ch)
}

func lineOffsets(text string) []int {
	offsets := []int{0}
	for i, r := range text {
		if r == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

func linesForRange(text string, offsets []int, start, end int) (startLine, endLine int64) {
	startLine = 1
	endLine = 1
	for i, off := range offsets {
		if off <= start {
			startLine = int64(i + 1)
		}
		if off < end {
			endLine = int64(i + 1)
		}
	}
	return startLine, endLine
}

type partialMeta struct {
	chunkStrategy string
	symbolKind    string
	symbolName    string
	parentScope   string
}

func emitPartialWindows(rel string, lines []string, startLine int64, cfg Config, counter token.TokenCounter, meta partialMeta, emit EmitFunc) error {
	window, overlap := normalizeWindow(cfg.WindowLines, cfg.OverlapLines)
	maxTokens := cfg.bodyBudget(counter, rel, meta.parentScope)
	for start := 0; start < len(lines); {
		end := start + window
		if end > len(lines) {
			end = len(lines)
		}
		if start >= end {
			break
		}
		for end > start+1 && counter != nil && maxTokens > 0 && counter.Count(strings.Join(lines[start:end], "\n")) > maxTokens {
			end--
		}
		snippet := strings.Join(lines[start:end], "\n")
		if strings.TrimSpace(snippet) == "" {
			if end == len(lines) {
				break
			}
			start++
			continue
		}
		sl := startLine + int64(start)
		el := sl + int64(end-start) - 1
		ch := &zvec.Chunk{
			DocID:         docID(rel, sl, el, meta.symbolName),
			RelativePath:  filepath.ToSlash(rel),
			StartLine:     sl,
			EndLine:       el,
			ChunkType:     chunkTypeForPath(rel),
			Name:          filepath.Base(rel),
			Snippet:       snippet,
			SymbolName:    meta.symbolName,
			SymbolKind:    meta.symbolKind,
			ParentScope:   meta.parentScope,
			ChunkStrategy: meta.chunkStrategy,
		}
		if err := emit(ch); err != nil {
			return err
		}
		if end == len(lines) {
			break
		}
		next := end - overlap
		if next <= start {
			next = start + 1
		}
		start = next
	}
	return nil
}

// headingHasNoOverlap reports whether line is a markdown heading (for tests).
func headingHasNoOverlap(line string) bool {
	_, _, ok := parseHeading(line)
	return ok
}

// isWordBoundaryRune exported for tests
func isWordBoundaryRune(r rune) bool {
	return unicode.IsSpace(r)
}
