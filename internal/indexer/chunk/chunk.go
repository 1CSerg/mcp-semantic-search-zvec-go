package chunk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

const (
	defaultWindowLines                = 40
	defaultOverlapLines               = 8
	defaultStreamThresholdBytes       = 256 * 1024  // 256 KiB
	defaultMaxLineBytes         int64 = 1024 * 1024 // 1 MiB
)

// Options configures chunk splitting.
type Options struct {
	WindowLines          int
	OverlapLines         int
	MaxFileBytes         int64
	StreamThresholdBytes int64
	MaxLineBytes         int64
}

// FileChunks splits in-memory file content into searchable chunks.
func FileChunks(relativePath string, content []byte, opts Options) []zvec.Chunk {
	content = stripUTF8BOM(content)
	content = normalizeLineEndings(content)
	lines := strings.Split(string(content), "\n")
	return slideWindow(relativePath, lines, opts)
}

// ReadAndChunk reads a file from disk and returns chunks.
func ReadAndChunk(root, relativePath string, opts Options) ([]zvec.Chunk, error) {
	abs, err := resolveWithinRoot(root, relativePath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if opts.MaxFileBytes > 0 && info.Size() > opts.MaxFileBytes {
		return nil, fmt.Errorf("file too large for indexing: %d bytes (max %d)", info.Size(), opts.MaxFileBytes)
	}
	threshold := opts.StreamThresholdBytes
	if threshold <= 0 {
		threshold = defaultStreamThresholdBytes
	}
	if info.Size() <= threshold {
		return readAllAndChunk(abs, relativePath, opts)
	}
	return streamChunkLegacy(abs, relativePath, opts)
}

func streamChunkLegacy(abs, relativePath string, opts Options) ([]zvec.Chunk, error) {
	var chunks []zvec.Chunk
	coll := newBatchCollector(len(chunks)+1, func(batch []zvec.Chunk) error {
		chunks = append(chunks, batch...)
		return nil
	})
	if err := streamChunkBatched(abs, relativePath, opts, coll); err != nil {
		return nil, err
	}
	if err := coll.flush(); err != nil {
		return nil, err
	}
	return chunks, nil
}

func readAllAndChunk(abs, relativePath string, opts Options) ([]zvec.Chunk, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	maxLine := opts.MaxLineBytes
	if maxLine <= 0 {
		maxLine = defaultMaxLineBytes
	}
	if err := checkMaxLineBytes(data, maxLine); err != nil {
		return nil, err
	}
	return FileChunks(relativePath, data, opts), nil
}

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

func normalizeWindowOpts(opts Options) (window, overlap int) {
	window = opts.WindowLines
	if window <= 0 {
		window = defaultWindowLines
	}
	overlap = opts.OverlapLines
	if overlap <= 0 {
		overlap = defaultOverlapLines
	}
	if overlap >= window {
		overlap = window / 4
	}
	return window, overlap
}

func chunkFromLineWindow(rel string, lines []string, startLine int64, chunkType string) *zvec.Chunk {
	if len(lines) == 0 {
		return nil
	}
	snippet := strings.Join(lines, "\n")
	if strings.TrimSpace(snippet) == "" {
		return nil
	}
	endLine := startLine + int64(len(lines)) - 1
	return &zvec.Chunk{
		DocID:        docID(rel, startLine, endLine),
		RelativePath: filepath.ToSlash(rel),
		StartLine:    startLine,
		EndLine:      endLine,
		ChunkType:    chunkType,
		Name:         filepath.Base(rel),
		Snippet:      snippet,
	}
}

func slideWindow(rel string, allLines []string, opts Options) []zvec.Chunk {
	window, overlap := normalizeWindowOpts(opts)
	if len(allLines) == 0 {
		return nil
	}
	chunkType := chunkTypeForPath(rel)
	var chunks []zvec.Chunk
	for start := 0; start < len(allLines); start += window - overlap {
		end := start + window
		if end > len(allLines) {
			end = len(allLines)
		}
		if start >= end {
			break
		}
		if ch := chunkFromLineWindow(rel, allLines[start:end], int64(start+1), chunkType); ch != nil {
			chunks = append(chunks, *ch)
		} else if end == len(allLines) {
			break
		}
		if end == len(allLines) {
			break
		}
	}
	return chunks
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

func docID(relativePath string, startLine, endLine int64) string {
	raw := fmt.Sprintf("%s:%d:%d", relativePath, startLine, endLine)
	sum := sha256.Sum256([]byte(raw))
	return "doc_" + hex.EncodeToString(sum[:])[:16]
}

func chunkTypeForPath(rel string) string {
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".md", ".markdown":
		return "markdown"
	case ".yaml", ".yml", ".json", ".toml":
		return "config"
	default:
		return "code"
	}
}
