package ast

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

var (
	dcsQueryBlockRE = regexp.MustCompile(`(?s)<query>(.*?)</query>`)
	sdblQueryStart  = regexp.MustCompile(`(?i)^\s*(ВЫБРАТЬ|SELECT)(\s|$)`)
)

// DCSQuery is an SDBL query block extracted from Data Composition Schema XML.
type DCSQuery struct {
	Text      string
	StartLine int64
}

// ExtractDCSQueries returns SDBL text blocks from Data Composition Schema XML.
func ExtractDCSQueries(content []byte) []string {
	queries := ExtractDCSQueriesWithLines(content)
	out := make([]string, 0, len(queries))
	for _, q := range queries {
		out = append(out, q.Text)
	}
	return out
}

// ExtractDCSQueriesWithLines returns SDBL query blocks with 1-based start lines.
func ExtractDCSQueriesWithLines(content []byte) []DCSQuery {
	idxs := dcsQueryBlockRE.FindAllSubmatchIndex(content, -1)
	if len(idxs) == 0 {
		return nil
	}
	out := make([]DCSQuery, 0, len(idxs))
	for _, loc := range idxs {
		if len(loc) < 4 {
			continue
		}
		text := strings.TrimSpace(string(content[loc[2]:loc[3]]))
		if text == "" || !looksLikeSDBLQuery(text) {
			continue
		}
		out = append(out, DCSQuery{
			Text:      text,
			StartLine: lineNumberAtByte(content, loc[0]),
		})
	}
	return out
}

// StripDCSQueryBlocks removes <query>...</query> blocks from DCS XML.
func StripDCSQueryBlocks(content []byte) []byte {
	return dcsQueryBlockRE.ReplaceAll(content, nil)
}

func lineNumberAtByte(content []byte, byteIdx int) int64 {
	if byteIdx <= 0 {
		return 1
	}
	if byteIdx > len(content) {
		byteIdx = len(content)
	}
	return int64(bytes.Count(content[:byteIdx], []byte("\n")) + 1)
}

// StripBSLQueryString removes 1C string delimiters and leading pipe characters per line.
func StripBSLQueryString(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "|") {
			trimmed = strings.TrimSpace(trimmed[1:])
		}
		lines[i] = trimmed
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func looksLikeSDBLQuery(text string) bool {
	return sdblQueryStart.MatchString(text)
}

func splitSDBLQueries(text string) []string {
	parts := splitSDBLAtSemicolon(text)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && looksLikeSDBLQuery(p) {
			out = append(out, p)
		}
	}
	return out
}

func splitSDBLAtSemicolon(text string) []string {
	var parts []string
	var b strings.Builder
	inString := false
	quote := byte(0)
	for i := 0; i < len(text); i++ {
		c := text[i]
		if inString {
			b.WriteByte(c)
			if c == quote {
				if i+1 < len(text) && text[i+1] == quote {
					b.WriteByte(text[i+1])
					i++
					continue
				}
				inString = false
			}
			continue
		}
		switch c {
		case '"', '\'':
			inString = true
			quote = c
			b.WriteByte(c)
		case ';':
			parts = append(parts, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return parts
}

func firstSDBLQueryName(query string) string {
	for _, line := range strings.Split(query, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.FieldsFunc(line, func(r rune) bool {
			return unicode.IsSpace(r) || r == ','
		})
		if len(fields) >= 2 && (strings.EqualFold(fields[0], "ВЫБРАТЬ") || strings.EqualFold(fields[0], "SELECT")) {
			return fields[1]
		}
		break
	}
	return ""
}

// ChunkSDBLText emits query chunks from SDBL source (heuristic; tree-sitter SDBL not in v0.1.6).
func ChunkSDBLText(relativePath, queryText string, startLine int64, cfg Config, counter token.TokenCounter, parentScope string, emit EmitFunc) error {
	if counter == nil {
		counter = &token.HeuristicCounter{}
	}
	queries := splitSDBLQueries(queryText)
	if len(queries) == 0 {
		return nil
	}
	budget := cfg.bodyBudget(counter, relativePath, parentScope)
	rel := filepath.ToSlash(relativePath)

	emitQuery := func(text string, line int64) error {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		// Query boundaries are semantic units; do not drop small SDBL/DCS/embedded blocks
		// under min_chunk_tokens (would lose text already stripped from DCS XML).
		lines := strings.Split(text, "\n")
		endLine := line + int64(len(lines)) - 1
		if endLine < line {
			endLine = line
		}
		name := firstSDBLQueryName(text)
		if counter.Count(text) > budget {
			return emitPartialWindows(rel, lines, line, cfg, counter, partialMeta{
				chunkStrategy: "partial",
				symbolKind:    "query",
				symbolName:    name,
				parentScope:   parentScope,
			}, func(ch *zvec.Chunk) error {
				ch.ChunkType = "query"
				return emit(ch)
			})
		}
		ch := &zvec.Chunk{
			RelativePath:  rel,
			StartLine:     line,
			EndLine:       endLine,
			StartByte:     0,
			EndByte:       int64(len(text)),
			ChunkType:     "query",
			Name:          filepath.Base(rel),
			Snippet:       text,
			SymbolName:    name,
			SymbolKind:    "query",
			ParentScope:   parentScope,
			ChunkStrategy: "ast",
		}
		return emit(ch)
	}

	if len(queries) == 1 {
		return emitQuery(queries[0], startLine)
	}

	packageText := strings.Join(queries, ";\n")
	if counter.Count(packageText) <= budget {
		lines := strings.Split(packageText, "\n")
		endLine := startLine + int64(len(lines)) - 1
		ch := &zvec.Chunk{
			RelativePath:  rel,
			StartLine:     startLine,
			EndLine:       endLine,
			StartByte:     0,
			EndByte:       int64(len(packageText)),
			ChunkType:     "query",
			Name:          filepath.Base(rel),
			Snippet:       packageText,
			SymbolKind:    "query_package",
			ParentScope:   parentScope,
			ChunkStrategy: "ast",
		}
		return emit(ch)
	}

	line := startLine
	for _, q := range queries {
		if err := emitQuery(q, line); err != nil {
			return err
		}
		line += int64(strings.Count(q, "\n")) + 1
	}
	return nil
}
