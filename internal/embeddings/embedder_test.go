package embeddings

import (
	"fmt"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestNewEmbedderOpenAI(t *testing.T) {
	e, err := NewEmbedder(config.EmbeddingProfile{
		Provider: "openai_compatible",
		Model:    "test",
		BaseURL:  "http://127.0.0.1:9/v1",
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if e == nil {
		t.Fatal("expected embedder")
	}
}

func TestNewEmbedderUnsupported(t *testing.T) {
	_, err := NewEmbedder(config.EmbeddingProfile{Provider: "unknown"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewEmbedderONNXStub(t *testing.T) {
	_, err := NewEmbedder(config.EmbeddingProfile{
		Provider:  "onnx",
		ModelPath: t.TempDir(),
	}, t.TempDir())
	if err == nil {
		t.Fatal("expected error without onnx build tag")
	}
	if got := err.Error(); got == "" {
		t.Fatal("empty error")
	}
	if fmt.Sprintf("%v", err) == "" {
		t.Fatal("empty formatted error")
	}
}
