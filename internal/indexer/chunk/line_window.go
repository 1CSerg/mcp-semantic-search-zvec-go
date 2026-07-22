package chunk

import (
	"fmt"
	"os"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

// SlideWindowMeta carries metadata for line-window emission (including partial AST fallback).
type SlideWindowMeta struct {
	Window           int
	Overlap          int
	ChunkStrategy    string
	SymbolKind       string
	SymbolName       string
	ParentScope      string
	MaxTokens        int
	Counter          token.TokenCounter
	MinTokens        int
	ContentStartByte int64 // absolute StartByte of allLines[0] (0 for whole-file windows)
}

// NormalizeWindowLines returns effective window and overlap sizes.
func NormalizeWindowLines(window, overlap int) (int, int) {
	return normalizeWindowOpts(Options{WindowLines: window, OverlapLines: overlap})
}

// FileChunks splits in-memory file content into searchable chunks.
func FileChunks(relativePath string, content []byte, opts Options) []zvec.Chunk {
	var chunks []zvec.Chunk
	_ = FileChunksEmit(relativePath, content, opts, func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	return chunks
}

// FileChunksEmit streams chunks via emit callback.
func FileChunksEmit(relativePath string, content []byte, opts Options, emit EmitFunc) error {
	content = stripUTF8BOM(content)
	content = normalizeLineEndings(content)
	lines := strings.Split(string(content), "\n")
	window, overlap := normalizeWindowOpts(opts)
	return SlideWindowEmit(relativePath, lines, 1, SlideWindowMeta{
		Window:        window,
		Overlap:       overlap,
		ChunkStrategy: "line_window",
	}, emit)
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
	coll := newBatchCollector(32, func(batch []zvec.Chunk) error {
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

// SlideWindowEmit emits line-window chunks through callback.
func SlideWindowEmit(rel string, allLines []string, startLineOffset int64, meta SlideWindowMeta, emit EmitFunc) error {
	window, overlap := meta.Window, meta.Overlap
	if window <= 0 {
		window = defaultWindowLines
	}
	if overlap <= 0 {
		overlap = defaultOverlapLines
	}
	if overlap >= window {
		overlap = window / 4
	}
	if len(allLines) == 0 {
		return nil
	}
	counter := meta.Counter
	minTokens := meta.MinTokens
	lineOffsets := make([]int64, len(allLines))
	off := meta.ContentStartByte
	for i, line := range allLines {
		lineOffsets[i] = off
		off += int64(len(line))
		if i < len(allLines)-1 {
			off++
		}
	}
	for start := 0; start < len(allLines); {
		end := start + window
		if end > len(allLines) {
			end = len(allLines)
		}
		if start >= end {
			break
		}
		windowMeta := meta
		windowMeta.ContentStartByte = lineOffsets[start]
		if counter != nil && meta.MaxTokens > 0 {
			lo, hi := start+1, end
			for lo < hi {
				mid := lo + (hi-lo+1)/2
				startLine := startLineOffset + int64(start)
				ch := chunkFromLineWindow(rel, allLines[start:mid], startLine, windowMeta)
				if ch != nil && counter.Count(ch.Snippet) > meta.MaxTokens {
					hi = mid - 1
				} else {
					lo = mid
				}
			}
			end = lo
		}
		startLine := startLineOffset + int64(start)
		ch := chunkFromLineWindow(rel, allLines[start:end], startLine, windowMeta)
		if ch != nil {
			if counter != nil && minTokens > 0 && counter.Count(ch.Snippet) < minTokens {
				if end == len(allLines) {
					break
				}
				start++
				continue
			}
			if err := emit(ch); err != nil {
				return err
			}
		} else if end == len(allLines) {
			break
		}
		if end == len(allLines) {
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
