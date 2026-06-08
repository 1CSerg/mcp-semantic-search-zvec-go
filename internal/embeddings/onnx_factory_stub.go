//go:build !onnx

package embeddings

import (
	"fmt"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func newONNXClient(_ config.EmbeddingProfile, _ string) (Embedder, error) {
	return nil, fmt.Errorf("onnx provider requires build tag -tags onnx and ONNX Runtime libraries")
}
