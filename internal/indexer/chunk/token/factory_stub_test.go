//go:build !onnx

package token

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestNewCounterONNXStub(t *testing.T) {
	c, err := NewCounter(config.EmbeddingProfile{Provider: "onnx"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.Count("test") == 0 && c.Count("abcdefgh") == 0 {
		t.Fatal("expected non-zero counts from heuristic fallback")
	}
}
