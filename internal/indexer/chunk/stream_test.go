package chunk

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func writeTempFile(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.go")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertChunksEqual(t *testing.T, inline, streamed []zvec.Chunk) {
	t.Helper()
	if len(inline) != len(streamed) {
		t.Fatalf("chunk count inline=%d streamed=%d\ninline=%+v\nstreamed=%+v", len(inline), len(streamed), inline, streamed)
	}
	inline = normalizeChunkDocIDs(inline)
	streamed = normalizeChunkDocIDs(streamed)
	for i := range inline {
		if inline[i] != streamed[i] {
			t.Fatalf("chunk[%d]\ninline=%+v\nstreamed=%+v", i, inline[i], streamed[i])
		}
	}
}

func normalizeChunkDocIDs(chunks []zvec.Chunk) []zvec.Chunk {
	out := make([]zvec.Chunk, len(chunks))
	for i, ch := range chunks {
		out[i] = ch
		out[i].DocID = DocIDForChunk(&ch, i+1)
	}
	return out
}

func TestStreamChunkMatchesFileChunks(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
		opts    Options
	}{
		{
			name:    "basic",
			content: []byte("line1\nline2\nline3\nline4\nline5\n"),
			opts:    Options{WindowLines: 3, OverlapLines: 1},
		},
		{
			name:    "bom",
			content: []byte{0xEF, 0xBB, 0xBF, 'p', 'a', 'c', 'k', 'a', 'g', 'e', '\n', 'x', '\n'},
			opts:    Options{WindowLines: 10},
		},
		{
			name:    "crlf",
			content: []byte("line1\r\nline2\r\nline3\r\n"),
			opts:    Options{WindowLines: 2, OverlapLines: 1},
		},
		{
			name:    "whitespace-only",
			content: []byte("\n\n\n"),
			opts:    Options{WindowLines: 2, OverlapLines: 1},
		},
		{
			name:    "exact-window",
			content: []byte("a\nb\nc\n"),
			opts:    Options{WindowLines: 3, OverlapLines: 1},
		},
		{
			name:    "overlap-defaults",
			content: []byte(strings.Repeat("x\n", 50)),
			opts:    Options{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rel := "pkg/f.go"
			inline := FileChunks(rel, tc.content, tc.opts)
			path := writeTempFile(t, tc.content)
			streamed, err := streamChunk(path, rel, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			assertChunksEqual(t, inline, streamed)
		})
	}
}

func TestReadAndChunkUsesStreamingPath(t *testing.T) {
	root := t.TempDir()
	rel := "large.go"
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	content := []byte(b.String())
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		StreamThresholdBytes: 1024,
		MaxFileBytes:         int64(len(content)) + 1,
	}
	inline := FileChunks(rel, content, opts)
	streamed, err := ReadAndChunk(root, rel, opts)
	if err != nil {
		t.Fatal(err)
	}
	assertChunksEqual(t, inline, streamed)
}

func TestStreamChunkRejectsLongLine(t *testing.T) {
	content := []byte(strings.Repeat("x", 128) + "\n")
	path := writeTempFile(t, content)
	_, err := streamChunk(path, "f.go", Options{MaxLineBytes: 64})
	if err == nil {
		t.Fatal("expected long line error")
	}
	if !strings.Contains(err.Error(), "line too long for indexing") {
		t.Fatalf("err=%v", err)
	}
}

func TestReadAndChunkEmptyFileStreaming(t *testing.T) {
	root := t.TempDir()
	rel := "empty.go"
	if err := os.WriteFile(filepath.Join(root, rel), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{StreamThresholdBytes: 0}
	chunks, err := ReadAndChunk(root, rel, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("chunks=%v", chunks)
	}
}

func TestReadAndChunkSmallFileInlinePath(t *testing.T) {
	root := t.TempDir()
	rel := "small.go"
	content := []byte("package main\n")
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{StreamThresholdBytes: 1 << 20}
	chunks, err := ReadAndChunk(root, rel, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := FileChunks(rel, content, opts)
	assertChunksEqual(t, want, chunks)
}

func TestStreamChunkNoTrailingNewline(t *testing.T) {
	content := []byte("line1\nline2")
	path := writeTempFile(t, content)
	streamed, err := streamChunk(path, "f.go", Options{WindowLines: 2, OverlapLines: 0})
	if err != nil {
		t.Fatal(err)
	}
	inline := FileChunks("f.go", content, Options{WindowLines: 2, OverlapLines: 0})
	assertChunksEqual(t, inline, streamed)
}

func TestStreamChunkClassicMacLineEnding(t *testing.T) {
	content := []byte("line1\rline2\r")
	path := writeTempFile(t, content)
	streamed, err := streamChunk(path, "f.go", Options{WindowLines: 2, OverlapLines: 0})
	if err != nil {
		t.Fatal(err)
	}
	inline := FileChunks("f.go", content, Options{WindowLines: 2, OverlapLines: 0})
	assertChunksEqual(t, inline, streamed)
}

func streamChunk(abs, relativePath string, opts Options) ([]zvec.Chunk, error) {
	return streamChunkLegacy(abs, relativePath, opts)
}
