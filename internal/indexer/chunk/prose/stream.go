package prose

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
)

// StreamBatched reads a large prose file line-by-line and emits chunks without loading the whole file.
func StreamBatched(abs, relativePath string, cfg Config, counter token.TokenCounter, emit EmitFunc) error {
	f, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	maxLine := int64(1024 * 1024)
	reader, err := newStreamLineReader(f, maxLine)
	if err != nil {
		return err
	}

	st := newMDStreamState(relativePath, cfg, counter, emit)
	for {
		line, err := reader.readLine()
		if err == io.EOF {
			return st.finish()
		}
		if err != nil {
			return err
		}
		if err := st.feedLine(line); err != nil {
			return err
		}
	}
}

type mdStreamState struct {
	rel     string
	cfg     Config
	counter token.TokenCounter
	emit    EmitFunc
	em      *proseEmitter
	parser  *mdState
}

func newMDStreamState(rel string, cfg Config, counter token.TokenCounter, emit EmitFunc) *mdStreamState {
	return &mdStreamState{
		rel:     rel,
		cfg:     cfg,
		counter: counter,
		emit:    emit,
		em:      newProseEmitter(rel, cfg, counter, emit),
		parser:  newMDState(),
	}
}

func (s *mdStreamState) feedLine(line string) error {
	segs := s.parser.feedLine(line)
	for _, seg := range segs {
		if err := s.em.emitSegment(seg); err != nil {
			return err
		}
	}
	return nil
}

func (s *mdStreamState) finish() error {
	for _, seg := range s.parser.flushAll() {
		if err := s.em.emitSegment(seg); err != nil {
			return err
		}
	}
	return nil
}

type streamLineReader struct {
	r            *bufio.Reader
	maxLine      int64
	pendingEmpty bool
}

func newStreamLineReader(r io.Reader, maxLine int64) (*streamLineReader, error) {
	lr := &streamLineReader{r: bufio.NewReader(r), maxLine: maxLine}
	if bom, err := lr.r.Peek(3); err == nil && len(bom) == 3 &&
		bom[0] == 0xEF && bom[1] == 0xBB && bom[2] == 0xBF {
		if _, err := lr.r.Discard(3); err != nil {
			return nil, err
		}
	}
	return lr, nil
}

func (lr *streamLineReader) readLine() (string, error) {
	if lr.pendingEmpty {
		lr.pendingEmpty = false
		return "", nil
	}
	var line []byte
	var size int64
	for {
		b, err := lr.r.ReadByte()
		if err == io.EOF {
			if len(line) == 0 {
				return "", io.EOF
			}
			return string(line), nil
		}
		if err != nil {
			return "", err
		}
		size++
		if size > lr.maxLine {
			return "", fmt.Errorf("line too long for indexing: exceeds %d bytes", lr.maxLine)
		}
		if b == '\n' {
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lr.markTrailingEmptyAfterNewline()
			return string(line), nil
		}
		if b == '\r' {
			if peek, err := lr.r.Peek(1); err == nil && len(peek) == 1 && peek[0] == '\n' {
				if _, err := lr.r.ReadByte(); err != nil {
					return "", err
				}
				if len(line) > 0 && line[len(line)-1] == '\r' {
					line = line[:len(line)-1]
				}
				lr.markTrailingEmptyAfterNewline()
				return string(line), nil
			}
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lr.markTrailingEmptyAfterNewline()
			return string(line), nil
		}
		line = append(line, b)
	}
}

func (lr *streamLineReader) markTrailingEmptyAfterNewline() {
	if lr.r.Buffered() == 0 {
		if _, err := lr.r.Peek(1); err == io.EOF {
			lr.pendingEmpty = true
		}
	}
}
