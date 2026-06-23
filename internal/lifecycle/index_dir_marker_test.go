package lifecycle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchesStdioIndexDirViaMarker(t *testing.T) {
	staging := t.TempDir()
	indexDir := filepath.Join(staging, "index-data")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(staging, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, indexDirMarkerFile), []byte(indexDir), 0o644); err != nil {
		t.Fatal(err)
	}

	exe := filepath.Join(binDir, "mcp-semantic-search-zvec-go.exe")
	cmdline := `"` + exe + `" --stdio`
	if !matchesStdioIndexDir(cmdline, indexDir, 5678, 1234) {
		t.Fatal("expected match via index-dir marker")
	}
	if matchesStdioIndexDir(cmdline, filepath.Join(staging, "other"), 5678, 1234) {
		t.Fatal("expected no match for different index dir")
	}
}

func TestIndexDirFromCmdline(t *testing.T) {
	staging := t.TempDir()
	indexDir := filepath.Join(staging, "idx")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(staging, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, indexDirMarkerFile), []byte(indexDir), 0o644); err != nil {
		t.Fatal(err)
	}

	exe := filepath.Join(binDir, "mcp-semantic-search-zvec-go")
	got := indexDirFromCmdline(`"` + exe + `" --stdio`)
	if !pathsEqual(got, indexDir) {
		t.Fatalf("indexDirFromCmdline = %q want %q", got, indexDir)
	}
}
