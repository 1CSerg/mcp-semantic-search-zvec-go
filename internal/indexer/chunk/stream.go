package chunk

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

func streamChunkBatched(abs, relativePath string, opts Options, coll *batchCollector) error {
	f, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	window, overlap := normalizeWindowOpts(opts)
	maxLine := opts.MaxLineBytes
	if maxLine <= 0 {
		maxLine = defaultMaxLineBytes
	}

	reader, err := newLineReader(f, maxLine)
	if err != nil {
		return err
	}

	meta := SlideWindowMeta{Window: window, Overlap: overlap, ChunkStrategy: "line_window"}
	buf := make([]string, 0, window)
	start := 0
	lineCount := 0

	for {
		if len(buf) < window {
			line, err := reader.readLine()
			if err == io.EOF {
				if len(buf) == 0 {
					return nil
				}
				end := start + len(buf)
				if ch := chunkFromLineWindow(relativePath, buf, int64(start+1), meta); ch != nil {
					if err := coll.add(ch); err != nil {
						return err
					}
				} else if end == lineCount {
					return nil
				}
				return nil
			}
			if err != nil {
				return err
			}
			lineCount++
			buf = append(buf, line)
			continue
		}

		end := start + window
		ch := chunkFromLineWindow(relativePath, buf[:window], int64(start+1), meta)
		if ch != nil {
			if err := coll.add(ch); err != nil {
				return err
			}
		}

		line, err := reader.readLine()
		if err == io.EOF {
			if ch == nil && end == lineCount {
				return nil
			}
			return nil
		}
		if err != nil {
			return err
		}
		lineCount++

		step := window - overlap
		start += step
		buf = append(buf[step:window], line)
	}
}

type lineReader struct {
	r            *bufio.Reader
	maxLine      int64
	pendingEmpty bool
}

func newLineReader(r io.Reader, maxLine int64) (*lineReader, error) {
	lr := &lineReader{r: bufio.NewReader(r), maxLine: maxLine}
	if bom, err := lr.r.Peek(3); err == nil && len(bom) == 3 &&
		bom[0] == 0xEF && bom[1] == 0xBB && bom[2] == 0xBF {
		if _, err := lr.r.Discard(3); err != nil {
			return nil, err
		}
	}
	return lr, nil
}

func (lr *lineReader) readLine() (string, error) {
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

func (lr *lineReader) markTrailingEmptyAfterNewline() {
	if lr.r.Buffered() == 0 {
		if _, err := lr.r.Peek(1); err == io.EOF {
			lr.pendingEmpty = true
		}
	}
}
