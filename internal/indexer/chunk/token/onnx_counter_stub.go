//go:build !onnx

package token

import (
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func newONNXCounter(_ config.EmbeddingProfile, _ string) (TokenCounter, error) {
	return &HeuristicCounter{}, nil
}
