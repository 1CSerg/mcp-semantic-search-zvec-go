package onnx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestResolveBundleMissingPath(t *testing.T) {
	_, err := ResolveBundle(config.EmbeddingProfile{Provider: "onnx"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveBundleMissingFiles(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveBundle(config.EmbeddingProfile{
		Provider:  "onnx",
		ModelPath: dir,
	}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing model files")
	}
}

func TestResolveBundleSuccess(t *testing.T) {
	bundle := t.TempDir()
	writeBundle(t, bundle)
	paths, err := ResolveBundle(config.EmbeddingProfile{
		Provider:  "onnx",
		ModelPath: bundle,
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if paths.ModelFile == "" || paths.Tokenizer == "" {
		t.Fatalf("paths=%+v", paths)
	}
}

func TestResolveBundleRelativePath(t *testing.T) {
	ws := t.TempDir()
	bundleRel := filepath.Join(".mcp-semantic-search-zvec-go", "models", "test-model")
	bundleAbs := filepath.Join(ws, bundleRel)
	if err := os.MkdirAll(bundleAbs, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBundleAt(t, bundleAbs)
	paths, err := ResolveBundle(config.EmbeddingProfile{
		Provider:  "onnx",
		ModelPath: bundleRel,
	}, ws)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(bundleAbs)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Dir != want {
		t.Fatalf("dir=%q want %q", paths.Dir, want)
	}
}

func writeBundle(t *testing.T, dir string) {
	t.Helper()
	writeBundleAt(t, dir)
}

func writeBundleAt(t *testing.T, dir string) {
	t.Helper()
	for name, content := range map[string]string{
		modelFileName:     "onnx",
		tokenizerFileName: `{}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidateBundle(t *testing.T) {
	if err := ValidateBundle(BundlePaths{}); err == nil {
		t.Fatal("expected error")
	}
}
