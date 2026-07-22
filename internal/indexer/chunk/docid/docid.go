package docid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Params holds stable inputs for document identity (chunking v2).
type Params struct {
	RelativePath  string
	StartLine     int64
	EndLine       int64
	StartByte     int64
	EndByte       int64
	ChunkIndex    int
	ChunkStrategy string
	ChunkType     string
	SymbolName    string
	Snippet       string
}

// Make derives a stable unique document id from chunk metadata and content fingerprint.
func Make(p Params) string {
	fp := contentFingerprint(p.Snippet)
	raw := fmt.Sprintf("%s:%d:%d:%d:%d:%d:%s:%s:%s:%s",
		strings.ReplaceAll(p.RelativePath, "\\", "/"),
		p.StartLine, p.EndLine, p.StartByte, p.EndByte, p.ChunkIndex,
		p.ChunkStrategy, p.ChunkType, p.SymbolName, fp)
	sum := sha256.Sum256([]byte(raw))
	return "doc_" + hex.EncodeToString(sum[:])[:16]
}

func contentFingerprint(snippet string) string {
	normalized := strings.TrimSpace(snippet)
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:4])
}

// AssertUnique returns an error when ids contains duplicates.
func AssertUnique(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return fmt.Errorf("empty doc id")
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate doc id %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}
