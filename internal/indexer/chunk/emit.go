package chunk

import "github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"

// EmitFunc receives each produced chunk; production path streams via batchCollector.add.
type EmitFunc func(*zvec.Chunk) error
