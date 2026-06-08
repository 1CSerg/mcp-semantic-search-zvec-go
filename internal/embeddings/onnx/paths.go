package onnx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

const (
	modelFileName     = "model_optimized.onnx"
	tokenizerFileName = "tokenizer.json"
)

// BundlePaths holds resolved paths for an ONNX model bundle directory.
type BundlePaths struct {
	Dir       string
	ModelFile string
	Tokenizer string
}

// ResolveBundle resolves profile.model_path against workspaceRoot and validates files exist.
func ResolveBundle(profile config.EmbeddingProfile, workspaceRoot string) (BundlePaths, error) {
	raw := strings.TrimSpace(profile.ModelPath)
	if raw == "" {
		return BundlePaths{}, fmt.Errorf("model_path is required for onnx profile")
	}
	dir := raw
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(workspaceRoot, raw)
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return BundlePaths{}, fmt.Errorf("resolve model_path: %w", err)
	}
	paths := BundlePaths{
		Dir:       dir,
		ModelFile: filepath.Join(dir, modelFileName),
		Tokenizer: filepath.Join(dir, tokenizerFileName),
	}
	if err := ValidateBundle(paths); err != nil {
		return BundlePaths{}, err
	}
	return paths, nil
}

// ValidateBundle checks required bundle files exist.
func ValidateBundle(paths BundlePaths) error {
	if paths.Dir == "" {
		return fmt.Errorf("model bundle directory is empty")
	}
	info, err := os.Stat(paths.Dir)
	if err != nil {
		return fmt.Errorf("model bundle directory %q: %w", paths.Dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("model_path %q is not a directory", paths.Dir)
	}
	for _, p := range []struct {
		path string
		name string
	}{
		{paths.ModelFile, modelFileName},
		{paths.Tokenizer, tokenizerFileName},
	} {
		if _, err := os.Stat(p.path); err != nil {
			return fmt.Errorf("missing %s in model bundle %q: %w", p.name, paths.Dir, err)
		}
	}
	return nil
}
