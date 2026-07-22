package chunk

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/docid"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

const (
	defaultWindowLines                = 40
	defaultOverlapLines               = 8
	defaultStreamThresholdBytes       = 256 * 1024  // 256 KiB
	defaultMaxLineBytes         int64 = 1024 * 1024 // 1 MiB
)

// checkMaxLineBytes mirrors the streaming path's per-line limit so the in-memory
// path rejects over-long lines identically (matters when the stream threshold is
// configured above max_line_bytes). CR and LF reset the running line length.
func checkMaxLineBytes(content []byte, maxLine int64) error {
	if maxLine <= 0 {
		return nil
	}
	var lineLen int64
	for _, b := range content {
		if b == '\n' || b == '\r' {
			lineLen = 0
			continue
		}
		lineLen++
		if lineLen > maxLine {
			return fmt.Errorf("line too long for indexing: exceeds %d bytes", maxLine)
		}
	}
	return nil
}

// ResolveWithinRoot joins root and relativePath, resolves symlinks, and verifies
// the result stays under root (rejects "../" escapes, symlink escapes, and absolute injection).
func ResolveWithinRoot(root, relativePath string) (string, error) {
	return resolveWithinRoot(root, relativePath)
}

func resolveWithinRoot(root, relativePath string) (string, error) {
	rootClean := filepath.Clean(root)
	abs := filepath.Clean(filepath.Join(rootClean, filepath.FromSlash(relativePath)))
	if err := assertLexicalContainment(abs, rootClean, relativePath); err != nil {
		return "", err
	}

	rootReal, err := filepath.EvalSymlinks(rootClean)
	if err != nil {
		return "", fmt.Errorf("workspace root: %w", err)
	}

	absReal, err := resolveSymlinksSafe(abs, rootReal, relativePath)
	if err != nil {
		return "", err
	}
	return absReal, nil
}

func assertLexicalContainment(abs, rootClean, relativePath string) error {
	rootWithSep := rootClean + string(filepath.Separator)
	if abs != rootClean && !strings.HasPrefix(abs, rootWithSep) {
		return fmt.Errorf("path %q escapes workspace root", relativePath)
	}
	return nil
}

func assertRealContainment(path, rootReal, relativePath string) error {
	rootClean := filepath.Clean(rootReal)
	pathClean := filepath.Clean(path)
	rootWithSep := rootClean + string(filepath.Separator)
	if pathClean != rootClean && !strings.HasPrefix(pathClean, rootWithSep) {
		if relativePath != "" {
			return fmt.Errorf("path %q escapes workspace root", relativePath)
		}
		return fmt.Errorf("path escapes workspace root")
	}
	return nil
}

func resolveSymlinksSafe(abs, rootReal, relativePath string) (string, error) {
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", err
		}
		parentReal, parentErr := filepath.EvalSymlinks(parent)
		if parentErr != nil {
			return "", parentErr
		}
		if err := assertRealContainment(parentReal, rootReal, relativePath); err != nil {
			return "", err
		}
		return filepath.Join(parentReal, filepath.Base(abs)), nil
	}
	if err := assertRealContainment(real, rootReal, relativePath); err != nil {
		return "", err
	}
	return real, nil
}

func stripUTF8BOM(content []byte) []byte {
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		return content[3:]
	}
	return content
}

func normalizeLineEndings(content []byte) []byte {
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(content, []byte("\r"), []byte("\n"))
}

// DocIDParams mirrors docid.Params for callers in the chunk package.
type DocIDParams = docid.Params

// DocID derives a stable unique document id (chunking v2).
func DocID(p DocIDParams) string {
	return docid.Make(p)
}

// DocIDForChunk assigns a v2 doc id using chunk fields and a per-file ordinal.
func DocIDForChunk(ch *zvec.Chunk, ordinal int) string {
	if ch == nil {
		return ""
	}
	return docid.Make(docid.Params{
		RelativePath:  ch.RelativePath,
		StartLine:     ch.StartLine,
		EndLine:       ch.EndLine,
		StartByte:     ch.StartByte,
		EndByte:       ch.EndByte,
		ChunkIndex:    ordinal,
		ChunkStrategy: ch.ChunkStrategy,
		ChunkType:     ch.ChunkType,
		SymbolName:    ch.SymbolName,
		Snippet:       ch.Snippet,
	})
}

// AssertUniqueDocIDs fails when ids contains duplicates or empty values.
func AssertUniqueDocIDs(ids []string) error {
	return docid.AssertUnique(ids)
}

func chunkTypeForPath(rel string) string {
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".md", ".markdown", ".mdc", ".txt":
		return "markdown"
	case ".yaml", ".yml", ".json", ".toml":
		return "config"
	default:
		return "code"
	}
}

// prepareContent normalizes file bytes before chunking.
func prepareContent(content []byte) []byte {
	return normalizeLineEndings(stripUTF8BOM(content))
}

// chunkFromLineWindow builds a line-window chunk (used by stream.go and line_window.go).
func chunkFromLineWindow(rel string, lines []string, startLine int64, meta SlideWindowMeta) *zvec.Chunk {
	if len(lines) == 0 {
		return nil
	}
	snippet := strings.Join(lines, "\n")
	if strings.TrimSpace(snippet) == "" {
		return nil
	}
	endLine := startLine + int64(len(lines)) - 1
	strategy := meta.ChunkStrategy
	if strategy == "" {
		strategy = "line_window"
	}
	symbolName := meta.SymbolName
	return &zvec.Chunk{
		RelativePath:  filepath.ToSlash(rel),
		StartLine:     startLine,
		EndLine:       endLine,
		ChunkType:     chunkTypeForPath(rel),
		Name:          filepath.Base(rel),
		Snippet:       snippet,
		SymbolName:    symbolName,
		SymbolKind:    meta.SymbolKind,
		ParentScope:   meta.ParentScope,
		ChunkStrategy: strategy,
	}
}
