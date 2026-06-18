//go:build zvec && treesitter

package ast

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	errParseFailed = errors.New("parse failed")
)

const parseErrorRateThreshold = 0.30

type engine struct {
	rel            string
	src            []byte
	cfg            Config
	counter        token.TokenCounter
	emit           EmitFunc
	boundaries     map[uintptr]BoundaryMeta
	baseScope      Scope
	parentMeta     *BoundaryMeta
	enclosingScope Scope
	minTokens      int
}

// ChunkGo parses Go source and emits AST chunks via callback (no slice accumulation).
func ChunkGo(relativePath string, content []byte, cfg Config, counter token.TokenCounter, emit EmitFunc) error {
	if counter == nil {
		counter = &token.HeuristicCounter{}
	}
	parser, tree, err := goParserPool.parseTree(content)
	if err != nil {
		return err
	}
	defer tree.Close()
	defer goParserPool.release(parser)

	root := tree.RootNode()
	if root == nil || root.NamedChildCount() == 0 {
		return ErrEmptyTree
	}
	if root.HasError() && parseErrorRate(root) > parseErrorRateThreshold {
		return ErrHighParseErrorRate
	}

	boundaries, pkgScope, err := indexBoundaries(root, content)
	if err != nil {
		return err
	}

	minTokens := cfg.MinChunkTokens
	if minTokens <= 0 {
		minTokens = 10
	}

	eng := &engine{
		rel:        filepath.ToSlash(relativePath),
		src:        content,
		cfg:        cfg,
		counter:    counter,
		emit:       emit,
		boundaries: boundaries,
		baseScope:  pkgScope,
		minTokens:  minTokens,
	}
	return eng.walkChunk(root, nil, eng.baseScope, nil, eng.baseScope, true)
}

func parseErrorRate(root *sitter.Node) float64 {
	var total, errs int
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.IsNamed() {
			total++
			if n.IsError() {
				errs++
			}
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	if total == 0 {
		return 0
	}
	return float64(errs) / float64(total)
}

func (e *engine) walkChunk(node *sitter.Node, buffer []sitter.Node, scope Scope, parent *BoundaryMeta, enclosingScope Scope, emitGroupPreamble bool) error {
	e.parentMeta = parent
	e.enclosingScope = enclosingScope
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		if meta, ok := e.boundaries[child.Id()]; ok {
			if err := e.flushBuffer(&buffer, scope, parent, ""); err != nil {
				return err
			}
			ps := parentScopeForBoundary(scope, meta)
			budget := e.cfg.bodyBudget(e.counter, e.rel, ps)
			if e.tokenSize(child) <= budget {
				if err := e.emitBoundary(child, meta, scope, "ast"); err != nil {
					return err
				}
			} else {
				newScope := extendScope(scope, meta)
				if err := e.walkChunk(child, nil, newScope, &meta, scope, true); err != nil {
					return err
				}
			}
			continue
		}

		if hasBoundaryDescendant(child, e.boundaries) {
			if err := e.flushBuffer(&buffer, scope, parent, ""); err != nil {
				return err
			}
			if emitGroupPreamble {
				if err := e.emitPreambleBeforeFirstBoundary(child, scope); err != nil {
					return err
				}
			}
			if err := e.walkChunk(child, nil, scope, parent, enclosingScope, false); err != nil {
				return err
			}
			continue
		}

		ps := scope.String()
		budget := e.cfg.bodyBudget(e.counter, e.rel, ps)
		childSize := e.tokenSize(child)
		if childSize > budget {
			if err := e.flushBuffer(&buffer, scope, parent, ""); err != nil {
				return err
			}
			if child.NamedChildCount() == 0 || isIndivisibleStatement(child) {
				if err := e.emitPartial(child, scope, parent); err != nil {
					return err
				}
			} else if err := e.walkChunk(child, nil, scope, parent, enclosingScope, true); err != nil {
				return err
			}
			continue
		}

		bufSize := e.tokenSizeNodes(buffer)
		if bufSize+childSize > budget {
			if err := e.flushBuffer(&buffer, scope, parent, ""); err != nil {
				return err
			}
			buffer = append(buffer, *child)
		} else {
			buffer = append(buffer, *child)
		}
	}
	if err := e.flushBuffer(&buffer, scope, parent, ""); err != nil {
		return err
	}
	return e.emitTailAfterChildren(node, scope, parent)
}

func (e *engine) emitTailAfterChildren(node *sitter.Node, scope Scope, parent *BoundaryMeta) error {
	if node == nil || node.NamedChildCount() == 0 {
		return nil
	}
	last := node.NamedChild(node.NamedChildCount() - 1)
	if last == nil {
		return nil
	}
	tailStart := last.EndByte()
	if tailStart >= node.EndByte() {
		return nil
	}
	tail := string(e.src[tailStart:node.EndByte()])
	if strings.TrimSpace(tail) == "" {
		return nil
	}
	if e.counter.Count(tail) < e.minTokens {
		return nil
	}
	start := int64(last.EndPosition().Row) + 1
	if tail[0] == '\n' {
		start++
	}
	end := int64(node.EndPosition().Row) + 1
	strategy := "ast"
	symbolName := ""
	symbolKind := ""
	parentScope := scope.String()
	if parent != nil {
		symbolName = parent.Name
		symbolKind = parent.Kind
		parentScope = parentScopeForBoundary(e.enclosingScope, *parent)
		if parent.Kind == "function" || parent.Kind == "method" {
			strategy = "partial"
		}
	}
	ch := &zvec.Chunk{
		DocID:         docID(e.rel, start, end, symbolName),
		RelativePath:  e.rel,
		StartLine:     start,
		EndLine:       end,
		ChunkType:     "code",
		Name:          filepath.Base(e.rel),
		Snippet:       strings.TrimLeft(tail, "\n"),
		SymbolName:    symbolName,
		SymbolKind:    symbolKind,
		ParentScope:   parentScope,
		ChunkStrategy: strategy,
	}
	return e.emit(ch)
}

func findFirstBoundaryDescendant(node *sitter.Node, boundaries map[uintptr]BoundaryMeta) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		if _, ok := boundaries[child.Id()]; ok {
			return child
		}
		if found := findFirstBoundaryDescendant(child, boundaries); found != nil {
			return found
		}
	}
	return nil
}

func (e *engine) emitPreambleBeforeFirstBoundary(node *sitter.Node, scope Scope) error {
	if node == nil {
		return nil
	}
	firstBound := findFirstBoundaryDescendant(node, e.boundaries)
	if firstBound == nil {
		return nil
	}
	if firstBound.StartPosition().Row == node.StartPosition().Row {
		return nil
	}
	pre := string(e.src[node.StartByte():firstBound.StartByte()])
	if strings.TrimSpace(pre) == "" {
		return nil
	}
	if e.counter.Count(pre) < e.minTokens {
		return nil
	}
	start := int64(node.StartPosition().Row) + 1
	end := int64(firstBound.StartPosition().Row)
	if end < start {
		end = start
	}
	ch := &zvec.Chunk{
		DocID:         docID(e.rel, start, end, ""),
		RelativePath:  e.rel,
		StartLine:     start,
		EndLine:       end,
		ChunkType:     "code",
		Name:          filepath.Base(e.rel),
		Snippet:       strings.TrimRight(pre, "\n"),
		ParentScope:   scope.String(),
		ChunkStrategy: "ast",
	}
	return e.emit(ch)
}

func hasBoundaryDescendant(node *sitter.Node, boundaries map[uintptr]BoundaryMeta) bool {
	if node == nil {
		return false
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		if _, ok := boundaries[child.Id()]; ok {
			return true
		}
		if hasBoundaryDescendant(child, boundaries) {
			return true
		}
	}
	return false
}

func (e *engine) flushBuffer(buffer *[]sitter.Node, scope Scope, parent *BoundaryMeta, strategy string) error {
	if buffer == nil || len(*buffer) == 0 {
		return nil
	}
	nodes := *buffer
	text := verbatimConcat(e.src, nodes)
	if strings.TrimSpace(text) == "" {
		*buffer = nil
		return nil
	}
	if e.counter.Count(text) < e.minTokens {
		*buffer = nil
		return nil
	}
	if strategy == "" {
		strategy = "ast"
	}
	symbolKind := symbolKindForBufferNodes(nodes)
	ch := e.chunkFromNodes(nodes, scope, symbolKind, "", strategy)
	if ch == nil {
		*buffer = nil
		return nil
	}
	if err := e.emit(ch); err != nil {
		return err
	}
	*buffer = nil
	return nil
}

func (e *engine) emitBoundary(node *sitter.Node, meta BoundaryMeta, scope Scope, strategy string) error {
	ch := e.chunkFromNodes([]sitter.Node{*node}, scope, meta.Kind, meta.Name, strategy)
	if ch == nil {
		return nil
	}
	ch.ParentScope = parentScopeForBoundary(scope, meta)
	return e.emit(ch)
}

func (e *engine) emitPartial(node *sitter.Node, scope Scope, parent *BoundaryMeta) error {
	slog.Debug("chunk_partial", "path", e.rel, "kind", node.Kind())
	text := node.Utf8Text(e.src)
	lines := strings.Split(text, "\n")
	parentKind := ""
	parentName := ""
	if parent != nil {
		parentKind = parent.Kind
		parentName = parent.Name
	}
	ps := scope.String()
	if parent != nil {
		ps = parentScopeForBoundary(e.enclosingScope, *parent)
	}
	return emitPartialWindows(e.rel, lines, int64(node.StartPosition().Row)+1, e.cfg, e.counter, partialMeta{
		chunkStrategy: "partial",
		symbolKind:    parentKind,
		symbolName:    parentName,
		parentScope:   ps,
	}, e.emit)
}

func (e *engine) chunkFromNodes(nodes []sitter.Node, scope Scope, symbolKind, symbolName, strategy string) *zvec.Chunk {
	if len(nodes) == 0 {
		return nil
	}
	text := verbatimConcat(e.src, nodes)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	start := nodes[0].StartPosition().Row + 1
	end := nodes[len(nodes)-1].EndPosition().Row + 1
	parentScope := scope.String()
	if symbolKind == "method" {
		parentScope = parentScopeForBoundary(scope, BoundaryMeta{
			Kind:     symbolKind,
			Name:     symbolName,
			Captures: e.boundaries[nodes[0].Id()].Captures,
		})
	}
	return &zvec.Chunk{
		DocID:         docID(e.rel, int64(start), int64(end), symbolName),
		RelativePath:  e.rel,
		StartLine:     int64(start),
		EndLine:       int64(end),
		ChunkType:     "code",
		Name:          filepath.Base(e.rel),
		Snippet:       text,
		SymbolName:    symbolName,
		SymbolKind:    symbolKind,
		ParentScope:   parentScope,
		ChunkStrategy: strategy,
	}
}

func (e *engine) tokenSize(node *sitter.Node) int {
	return e.counter.Count(node.Utf8Text(e.src))
}

func (e *engine) tokenSizeNodes(nodes []sitter.Node) int {
	if len(nodes) == 0 {
		return 0
	}
	return e.counter.Count(verbatimConcat(e.src, nodes))
}

func verbatimConcat(src []byte, nodes []sitter.Node) string {
	if len(nodes) == 0 {
		return ""
	}
	start := nodes[0].StartByte()
	end := nodes[len(nodes)-1].EndByte()
	if int(end) > len(src) {
		end = uint(len(src))
	}
	return string(src[start:end])
}

// symbolKindForBufferNodes tags buffer flushes that include import_declaration nodes (§7).
func symbolKindForBufferNodes(nodes []sitter.Node) string {
	for _, n := range nodes {
		if n.Kind() == "import_declaration" {
			return "import"
		}
	}
	return ""
}

func isIndivisibleStatement(node *sitter.Node) bool {
	switch node.Kind() {
	case "if_statement", "for_statement", "switch_statement", "select_statement",
		"expression_statement", "return_statement", "assign_statement", "short_var_declaration",
		"inc_statement", "dec_statement", "go_statement", "defer_statement":
		return true
	default:
		return false
	}
}

// ParseErrorRate exposes parse error rate for tests.
func ParseErrorRate(root *sitter.Node) float64 {
	return parseErrorRate(root)
}

// ParseGoTree parses Go for tests; caller must run cleanup (Close tree + release parser).
func ParseGoTree(src []byte) (*sitter.Tree, func(), error) {
	parser, tree, err := goParserPool.parseTree(src)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		tree.Close()
		goParserPool.release(parser)
	}
	return tree, cleanup, nil
}

// IndexGoBoundaries exposes boundary indexing for unit tests.
func IndexGoBoundaries(root *sitter.Node, src []byte) (map[uintptr]BoundaryMeta, Scope, error) {
	return indexBoundaries(root, src)
}

// FormatTestError helps tests report walk failures.
func FormatTestError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
