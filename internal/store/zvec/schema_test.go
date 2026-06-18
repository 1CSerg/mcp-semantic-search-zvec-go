//go:build zvec

package zvec

import (
	"testing"
)

func TestChunkToDocDocToSearchHitSymbolFields(t *testing.T) {
	chunk := Chunk{
		DocID:         "doc1",
		RelativePath:  "main.go",
		StartLine:     1,
		EndLine:       10,
		ChunkType:     "code",
		Name:          "main",
		Snippet:       "func main() {}",
		SymbolName:    "main",
		SymbolKind:    "function",
		ParentScope:   "package main",
		ChunkStrategy: "hybrid",
	}
	vector := make([]float32, 3)

	doc, err := chunkToDoc(chunk, vector)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Destroy()

	hit := docToSearchHit(doc)
	if hit.SymbolName != chunk.SymbolName ||
		hit.SymbolKind != chunk.SymbolKind ||
		hit.ParentScope != chunk.ParentScope ||
		hit.ChunkStrategy != chunk.ChunkStrategy {
		t.Fatalf("hit=%+v chunk=%+v", hit, chunk)
	}
}
