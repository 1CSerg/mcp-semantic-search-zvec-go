//go:build onnx

package embeddings

import (
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/embeddings/onnx"
)

func newONNXClient(profile config.EmbeddingProfile, workspaceRoot string) (Embedder, error) {
	return onnx.NewClient(profile, workspaceRoot)
}
