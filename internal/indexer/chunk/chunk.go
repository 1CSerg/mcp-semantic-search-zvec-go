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

const defaultWindowLines = 40
const defaultOverlapLines = 8

// Options configures chunk splitting.
type Options struct {
	WindowLines  int
	OverlapLines int
}

// FileChunks splits a file into searchable chunks.
func FileChunks(relativePath string, content []byte, opts Options) []zvec.Chunk {
	content = stripUTF8BOM(content)
	content = normalizeLineEndings(content)
	window := opts.WindowLines
	if window <= 0 {
		window = defaultWindowLines
	}
	overlap := opts.OverlapLines
	if overlap <= 0 {
		overlap = defaultOverlapLines
	}
	if overlap >= window {
		overlap = window / 4
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 {
		return nil
	}
	chunkType := chunkTypeForPath(relativePath)
	var chunks []zvec.Chunk
	for start := 0; start < len(lines); start += window - overlap {
		end := start + window
		if end > len(lines) {
			end = len(lines)
		}
		if start >= end {
			break
		}
		snippet := strings.Join(lines[start:end], "\n")
		if strings.TrimSpace(snippet) == "" {
			if end == len(lines) {
				break
			}
			continue
		}
		startLine := int64(start + 1)
		endLine := int64(end)
		docID := docID(relativePath, startLine, endLine)
		name := filepath.Base(relativePath)
		chunks = append(chunks, zvec.Chunk{
			DocID:        docID,
			RelativePath: filepath.ToSlash(relativePath),
			StartLine:    startLine,
			EndLine:      endLine,
			ChunkType:    chunkType,
			Name:         name,
			Snippet:      snippet,
		})
		if end == len(lines) {
			break
		}
	}
	return chunks
}

// ReadAndChunk reads a file from disk and returns chunks.
func ReadAndChunk(root, relativePath string, opts Options) ([]zvec.Chunk, error) {
	abs, err := resolveWithinRoot(root, relativePath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	return FileChunks(relativePath, data, opts), nil
}

// resolveWithinRoot joins root and relativePath and verifies the result stays
// under root, rejecting "../" escapes and absolute path injection.
func resolveWithinRoot(root, relativePath string) (string, error) {
	rootClean := filepath.Clean(root)
	abs := filepath.Clean(filepath.Join(rootClean, filepath.FromSlash(relativePath)))
	rootWithSep := rootClean + string(filepath.Separator)
	if abs != rootClean && !strings.HasPrefix(abs, rootWithSep) {
		return "", fmt.Errorf("path %q escapes workspace root", relativePath)
	}
	return abs, nil
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
