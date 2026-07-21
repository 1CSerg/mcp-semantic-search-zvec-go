//go:build onnx

package token

import (
	"fmt"
	"log/slog"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/embeddings/onnx"
	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
)

// ONNXCounter uses the profile tokenizer for token counts.
type ONNXCounter struct {
	tok *tokenizer.Tokenizer
}

func newONNXCounter(profile config.EmbeddingProfile, workspaceRoot string) (TokenCounter, error) {
	paths, err := onnx.ResolveBundle(profile, workspaceRoot)
	if err != nil {
		return nil, err
	}
	tok, err := pretrained.FromFile(paths.Tokenizer)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}
	return &ONNXCounter{tok: tok}, nil
}

func (c *ONNXCounter) Count(text string) int {
	if c == nil || c.tok == nil {
		return 0
	}
	enc, err := c.tok.EncodeSingle(text, true)
	if err != nil {
		slog.Debug("onnx tokenizer count failed; using rune estimate", "err", err)
		return len([]rune(text)) / 4
	}
	return len(enc.Ids)
}
