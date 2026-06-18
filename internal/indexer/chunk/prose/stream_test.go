package prose

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

type chunkReader struct {
	data      []byte
	pos       int
	chunkSize int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := r.chunkSize
	if n > len(r.data)-r.pos {
		n = len(r.data) - r.pos
	}
	if n > len(p) {
		n = len(p)
	}
	copy(p, r.data[r.pos:r.pos+n])
	r.pos += n
	return n, nil
}

func streamFromChunks(t *testing.T, content string, chunkSize int) []zvec.Chunk {
	t.Helper()
	rel := "stream.md"
	var chunks []zvec.Chunk
	st := newMDStreamState(rel, testCfg(120), token.CharCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	reader, err := newStreamLineReader(&chunkReader{data: []byte(content), chunkSize: chunkSize}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	for {
		line, err := reader.readLine()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := st.feedLine(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.finish(); err != nil {
		t.Fatal(err)
	}
	return chunks
}

func TestStreamCrossBufferBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		content string
		check   func(t *testing.T, chunks []zvec.Chunk)
	}{
		{
			name:    "code_fence_split",
			content: "# Doc\n\n```go\nfmt.Println(\"x\")\n```\n",
			check: func(t *testing.T, chunks []zvec.Chunk) {
				found := false
				for _, ch := range chunks {
					if ch.SymbolKind == "code_block" {
						found = true
					}
				}
				if !found {
					t.Fatal("expected code_block across chunked reads")
				}
			},
		},
		{
			name:    "table_split",
			content: "| A | B |\n| - | - |\n| 1 | 2 |\n\nTail.\n",
			check: func(t *testing.T, chunks []zvec.Chunk) {
				if len(chunks) == 0 {
					t.Fatal("expected table chunks")
				}
			},
		},
		{
			name:    "front_matter_split",
			content: "---\nkey: value\n---\n\n# Title\n\nBody.\n",
			check: func(t *testing.T, chunks []zvec.Chunk) {
				foundFM := false
				foundSection := false
				for _, ch := range chunks {
					if ch.SymbolKind == "section" && strings.Contains(ch.Snippet, "key: value") {
						foundFM = true
					}
					if ch.SymbolKind == "section" && strings.Contains(ch.Snippet, "# Title") {
						foundSection = true
					}
				}
				if !foundFM {
					t.Fatal("expected front matter chunk")
				}
				if !foundSection {
					t.Fatal("expected heading section chunk")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunks := streamFromChunks(t, tc.content, 3)
			tc.check(t, chunks)
		})
	}
}

func TestStreamBatchedMarkdown(t *testing.T) {
	root := t.TempDir()
	rel := "big.md"
	var b strings.Builder
	b.WriteString("---\nkey: value\n---\n\n# Title\n\n")
	for i := 0; i < 500; i++ {
		b.WriteString("Paragraph line with content.\n\n")
	}
	b.WriteString("```go\nfmt.Println(\"x\")\n```\n")
	if err := os.WriteFile(filepath.Join(root, rel), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testCfg(120)
	var chunks []zvec.Chunk
	err := StreamBatched(filepath.Join(root, rel), rel, cfg, token.CharCounter{}, func(ch *zvec.Chunk) error {
		if ch != nil {
			chunks = append(chunks, *ch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected streamed chunks")
	}
	foundCode := false
	foundSection := false
	for _, ch := range chunks {
		if ch.SymbolKind == "code_block" {
			foundCode = true
		}
		if ch.SymbolKind == "section" {
			foundSection = true
		}
	}
	if !foundCode {
		t.Fatal("expected code_block from streamed fence")
	}
	if !foundSection {
		t.Fatal("expected section from streamed headings/front matter")
	}
}

func TestStreamLineReaderBOM(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bom.txt")
	content := []byte{0xEF, 0xBB, 0xBF, 'h', 'i', '\n'}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lr, err := newStreamLineReader(f, 1024)
	if err != nil {
		t.Fatal(err)
	}
	line, err := lr.readLine()
	if err != nil {
		t.Fatal(err)
	}
	if line != "hi" {
		t.Fatalf("line=%q", line)
	}
}

func TestEmitProseLinesFrontMatter(t *testing.T) {
	longFM := "---\n" + strings.Repeat("key: value\n", 30) + "---\n"
	chunks := collectChunks(t, "fm.md", []byte(longFM+"# H\n\nBody\n"), testCfg(40))
	if len(chunks) < 2 {
		t.Fatalf("chunks=%d", len(chunks))
	}
}

func TestTableExitsOnNonTableLine(t *testing.T) {
	content := "| a | b |\n| - | - |\n| 1 | 2 |\n\nPlain text.\n"
	segs := parseMarkdownSegments(content)
	if len(segs) < 2 {
		t.Fatalf("segments=%d", len(segs))
	}
}

func TestNormalizeWindow(t *testing.T) {
	w, o := normalizeWindow(0, 0)
	if w != 40 || o != 8 {
		t.Fatalf("w=%d o=%d", w, o)
	}
}

func TestDocIDStable(t *testing.T) {
	a := docID("a.md", 1, 2, "H")
	b := docID("a.md", 1, 2, "H")
	if a != b {
		t.Fatal("doc id not stable")
	}
}
