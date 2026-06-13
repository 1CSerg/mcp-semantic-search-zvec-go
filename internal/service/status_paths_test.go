package service

import (
	"path/filepath"
	"testing"
)

func TestStatusRelativePath(t *testing.T) {
	root := t.TempDir()
	absIndex := filepath.Join(root, ".mcp-semantic-search-zvec-go", "data", "index")

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "absolute under workspace",
			path: absIndex,
			want: ".mcp-semantic-search-zvec-go/data/index",
		},
		{
			name: "relative under workspace",
			path: ".mcp-semantic-search-zvec-go/config.yaml",
			want: ".mcp-semantic-search-zvec-go/config.yaml",
		},
		{
			name: "empty",
			path: "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := statusRelativePath(root, tc.path)
			if got != tc.want {
				t.Fatalf("statusRelativePath(%q, %q)=%q want %q", root, tc.path, got, tc.want)
			}
		})
	}
}

func TestRelativeIndexingMap(t *testing.T) {
	root := t.TempDir()
	absFile := filepath.Join(root, "src", "main.go")
	idx := map[string]any{
		"running":      true,
		"current_file": absFile,
	}
	out := relativeIndexingMap(root, idx)
	if out["current_file"] != "src/main.go" {
		t.Fatalf("current_file=%v", out["current_file"])
	}
}
