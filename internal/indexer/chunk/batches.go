package chunk

import (
	"fmt"
	"os"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/prose"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

// BatchFunc receives a batch of chunks produced from a single file.
type BatchFunc func(chunks []zvec.Chunk) error

type batchCollector struct {
	size    int
	acc     []zvec.Chunk
	emit    BatchFunc
	total   int
	ordinal int
	docIDs  []string
}

func newBatchCollector(batchSize int, fn BatchFunc) *batchCollector {
	if batchSize <= 0 {
		batchSize = 32
	}
	return &batchCollector{
		size:   batchSize,
		acc:    make([]zvec.Chunk, 0, batchSize),
		emit:   fn,
		docIDs: make([]string, 0, batchSize),
	}
}

func (b *batchCollector) add(ch *zvec.Chunk) error {
	if ch == nil {
		return nil
	}
	b.ordinal++
	chCopy := *ch
	chCopy.DocID = DocIDForChunk(&chCopy, b.ordinal)
	b.docIDs = append(b.docIDs, chCopy.DocID)
	b.acc = append(b.acc, chCopy)
	if len(b.acc) >= b.size {
		return b.flush()
	}
	return nil
}

func (b *batchCollector) flush() error {
	if len(b.acc) == 0 {
		return nil
	}
	batch := b.acc
	b.acc = make([]zvec.Chunk, 0, b.size)
	b.total += len(batch)
	return b.emit(batch)
}

func (b *batchCollector) totalChunks() int {
	return b.total + len(b.acc)
}

// ProcessBatches reads and chunks a file, invoking fn for each batch without holding
// all chunks for the file in memory at once.
func ProcessBatches(root, relativePath string, opts Options, counter token.TokenCounter, batchSize int, fn BatchFunc) (int, error) {
	abs, err := resolveWithinRoot(root, relativePath)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return 0, err
	}
	if opts.MaxFileBytes > 0 && info.Size() > opts.MaxFileBytes {
		return 0, fmt.Errorf("file too large for indexing: %d bytes (max %d)", info.Size(), opts.MaxFileBytes)
	}
	threshold := opts.StreamThresholdBytes
	if threshold <= 0 {
		threshold = defaultStreamThresholdBytes
	}

	coll := newBatchCollector(batchSize, fn)
	emit := func(ch *zvec.Chunk) error { return coll.add(ch) }
	router := NewChunkRouter()

	useStreaming := info.Size() > threshold && !hybridASTPath(relativePath, opts) && !hybridProsePath(relativePath, opts)
	if useStreaming {
		if err := streamChunkBatched(abs, relativePath, opts, coll); err != nil {
			return coll.totalChunks(), err
		}
	} else if info.Size() > threshold && hybridProsePath(relativePath, opts) {
		proseEmit := func(ch *zvec.Chunk) error { return emit(ch) }
		if err := prose.StreamBatched(abs, relativePath, proseConfig(opts), counter, proseEmit); err != nil {
			return coll.totalChunks(), err
		}
	} else {
		data, err := os.ReadFile(abs)
		if err != nil {
			return coll.totalChunks(), err
		}
		maxLine := opts.MaxLineBytes
		if maxLine <= 0 {
			maxLine = defaultMaxLineBytes
		}
		if err := checkMaxLineBytes(data, maxLine); err != nil {
			return coll.totalChunks(), err
		}
		if err := router.ChunkFile(relativePath, data, opts, counter, emit); err != nil {
			return coll.totalChunks(), err
		}
	}
	if err := coll.flush(); err != nil {
		return coll.totalChunks(), err
	}
	if err := AssertUniqueDocIDs(coll.docIDs); err != nil {
		return coll.totalChunks(), fmt.Errorf("%s: %w", relativePath, err)
	}
	return coll.totalChunks(), nil
}
