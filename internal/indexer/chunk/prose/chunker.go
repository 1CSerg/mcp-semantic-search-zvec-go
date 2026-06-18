package prose

import (
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
)

// ChunkFile routes plain text or markdown content through the prose chunker.
func ChunkFile(relativePath string, content []byte, cfg Config, counter token.TokenCounter, emit EmitFunc) error {
	content = normalizeContent(content)
	text := string(content)
	ext := strings.ToLower(filepath.Ext(relativePath))
	switch ext {
	case ".md", ".markdown", ".mdc":
		return ChunkMarkdown(relativePath, text, cfg, counter, emit)
	case ".txt":
		return ChunkPlainText(relativePath, text, cfg, counter, emit)
	default:
		return ChunkPlainText(relativePath, text, cfg, counter, emit)
	}
}

func normalizeContent(content []byte) []byte {
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}
	content = bytesReplaceCRLF(content)
	return content
}

func bytesReplaceCRLF(content []byte) []byte {
	out := make([]byte, 0, len(content))
	for i := 0; i < len(content); i++ {
		if content[i] == '\r' {
			if i+1 < len(content) && content[i+1] == '\n' {
				out = append(out, '\n')
				i++
				continue
			}
			out = append(out, '\n')
			continue
		}
		out = append(out, content[i])
	}
	return out
}

// ConfigFromOptions builds prose.Config from chunk router options.
func ConfigFromOptions(maxInput int, embedRatio, overlapRatio float64, contextPrefix bool, minTokens, window, overlap int) Config {
	return Config{
		MaxInputTokens:    maxInput,
		EmbedBudgetRatio:  embedRatio,
		ProseOverlapRatio: overlapRatio,
		ContextPrefix:     contextPrefix,
		MinChunkTokens:    minTokens,
		WindowLines:       window,
		OverlapLines:      overlap,
	}
}

// IsProsePath reports whether the extension should use prose chunking.
func IsProsePath(relativePath string) bool {
	ext := strings.ToLower(filepath.Ext(relativePath))
	switch ext {
	case ".md", ".markdown", ".mdc", ".txt":
		return true
	default:
		return false
	}
}
