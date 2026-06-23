package ast

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

// ErrNotImplemented is returned when tree-sitter AST chunking is not linked.
var ErrNotImplemented = errors.New("ast chunking not implemented: build with -tags zvec,treesitter and CGO_ENABLED=1")

// ErrEmptyTree is returned when parsing yields no named AST nodes.
var ErrEmptyTree = errors.New("empty ast tree")

// ErrHighParseErrorRate is returned when ERROR nodes exceed the fallback threshold.
var ErrHighParseErrorRate = errors.New("parse error rate too high")

// Config drives Go AST chunking budgets and fallback line windows.
type Config struct {
	MinChunkTokens   int
	MaxInputTokens   int
	EmbedBudgetRatio float64
	ContextPrefix    bool
	WindowLines      int
	OverlapLines     int
	IncludeSDBL      bool
}

// EmitFunc streams each produced chunk (production: batchCollector.add).
type EmitFunc func(*zvec.Chunk) error

func (c Config) bodyBudget(counter token.TokenCounter, rel, parentScope string) int {
	total := int(math.Floor(float64(c.MaxInputTokens) * c.EmbedBudgetRatio))
	if total <= 0 {
		total = c.MaxInputTokens
	}
	if !c.ContextPrefix || counter == nil {
		return total
	}
	prefix := contextPrefix(rel, parentScope)
	remain := total - counter.Count(prefix)
	if remain < 1 {
		return 1
	}
	return remain
}

func contextPrefix(relativePath, parentScope string) string {
	rel := strings.ReplaceAll(relativePath, "\\", "/")
	if parentScope == "" {
		return "// file: " + rel + "\n"
	}
	return "// file: " + rel + "\n// scope: " + parentScope + "\n"
}

func docID(relativePath string, startLine, endLine int64, symbolName string) string {
	raw := fmt.Sprintf("%s:%d:%d:%s", relativePath, startLine, endLine, symbolName)
	sum := sha256.Sum256([]byte(raw))
	return "doc_" + hex.EncodeToString(sum[:])[:16]
}

func normalizeWindow(window, overlap int) (int, int) {
	if window <= 0 {
		window = 40
	}
	if overlap <= 0 {
		overlap = 8
	}
	if overlap >= window {
		overlap = window / 4
	}
	return window, overlap
}

type partialMeta struct {
	chunkStrategy string
	symbolKind    string
	symbolName    string
	parentScope   string
	budgetScope   string // bodyBudget scope; defaults to parentScope when empty
}

func emitPartialWindows(rel string, lines []string, startLine int64, cfg Config, counter token.TokenCounter, meta partialMeta, emit EmitFunc) error {
	window, overlap := normalizeWindow(cfg.WindowLines, cfg.OverlapLines)
	minTokens := cfg.MinChunkTokens
	if minTokens <= 0 {
		minTokens = 10
	}
	budgetScope := meta.parentScope
	if meta.budgetScope != "" {
		budgetScope = meta.budgetScope
	}
	maxTokens := cfg.bodyBudget(counter, rel, budgetScope)
	for start := 0; start < len(lines); {
		end := start + window
		if end > len(lines) {
			end = len(lines)
		}
		if start >= end {
			break
		}
		// Shrink window until within token budget.
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
		if counter != nil && counter.Count(snippet) < minTokens {
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
			ChunkType:     "code",
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
