//go:build zvec

// seed-index creates a minimal zvec collection for local Phase 1 smoke tests.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func main() {
	os.Exit(run())
}

func run() int {
	var n int
	flag.IntVar(&n, "n", 100, "number of chunks to seed")
	flag.Parse()

	settings, err := config.Load()
	if err != nil {
		log.Printf("config: %v", err)
		return 1
	}
	profile, err := settings.ActiveProfile()
	if err != nil {
		log.Printf("profile: %v", err)
		return 1
	}
	if profile.Dimensions <= 0 {
		log.Printf("profile dimensions must be positive")
		return 1
	}

	cfg := zvec.Config{
		IndexDir:      settings.IndexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	store := zvec.New(cfg)
	defer func() { _ = store.Close() }()

	chunks := make([]zvec.Chunk, n)
	vectors := make([][]float32, n)
	for i := 0; i < n; i++ {
		chunks[i] = zvec.Chunk{
			DocID:        fmt.Sprintf("seed-%03d", i),
			RelativePath: fmt.Sprintf("internal/module/file_%d.go", i%10),
			StartLine:    int64(i * 10),
			EndLine:      int64(i*10 + 8),
			ChunkType:    "code",
			Name:         fmt.Sprintf("Func%d", i),
			Snippet:      fmt.Sprintf("seed snippet %d about authentication", i),
		}
		vec := make([]float32, profile.Dimensions)
		vec[i%profile.Dimensions] = 1
		vectors[i] = vec
	}

	if err := store.UpsertChunks(chunks, vectors); err != nil {
		log.Printf("upsert: %v", err)
		return 1
	}
	count, err := store.DocCount()
	if err != nil {
		log.Printf("doc count: %v", err)
		return 1
	}

	fmt.Printf("seeded %d chunks at %s\n", count, filepath.ToSlash(zvec.CollectionPath(cfg)))
	return 0
}
