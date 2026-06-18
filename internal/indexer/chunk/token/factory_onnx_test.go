//go:build onnx

package token

import (
	"os"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/embeddings/onnx"
)

func TestNewCounterONNX(t *testing.T) {
	root := os.Getenv("WORKSPACE_ROOT")
	if root == "" {
		root = "."
	}
	profile := config.EmbeddingProfile{
		Provider: "onnx",
		Model:    "local_multilingual",
	}
	paths, err := onnx.ResolveBundle(profile, root)
	if err != nil {
		t.Skipf("onnx bundle unavailable: %v", err)
	}
	profile.ModelPath = paths.Dir
	c, err := NewCounter(profile, root)
	if err != nil {
		t.Skipf("onnx counter unavailable: %v", err)
	}
	if got := c.Count("hello world"); got <= 0 {
		t.Fatalf("Count=%d", got)
	}
}
