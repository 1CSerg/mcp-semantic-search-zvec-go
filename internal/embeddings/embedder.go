package embeddings

import (
	"context"
	"fmt"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/embeddings/openai"
)

// Embedder produces vector embeddings for indexing and search.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
	Dimensions() int
	HealthCheck(ctx context.Context) error
}

// NewEmbedder creates an embedding provider from profile settings.
func NewEmbedder(profile config.EmbeddingProfile, workspaceRoot string) (Embedder, error) {
	switch profile.Provider {
	case "openai_compatible", "":
		return openai.NewClient(profile)
	case "onnx":
		return newONNXClient(profile, workspaceRoot)
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", profile.Provider)
	}
}
