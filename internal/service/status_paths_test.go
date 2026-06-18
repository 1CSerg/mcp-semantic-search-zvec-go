package service

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
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
		{
			name: "outside workspace",
			path: filepath.Join(root, "..", "other", "file.go"),
			want: "../other/file.go",
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

func TestRelativeIndexingMapNil(t *testing.T) {
	if relativeIndexingMap("/tmp", nil) != nil {
		t.Fatal("expected nil")
	}
}

func TestRelativeIndexingMapNoCurrentFile(t *testing.T) {
	idx := map[string]any{"running": true}
	got := relativeIndexingMap(t.TempDir(), idx)
	if got["running"] != true {
		t.Fatalf("got=%v", got)
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

func TestIndexStatusDiagnostics(t *testing.T) {
	root := t.TempDir()
	settings := &config.Settings{
		WorkspaceRoot: root,
		IndexDir:      filepath.Join(root, config.DefaultInstallDirName, config.DefaultIndexSubdir),
	}
	d := indexStatusDiagnostics(settings)
	if d["log_dir"] == "" {
		t.Fatalf("diagnostics=%v", d)
	}
	if d["log_file"] == "" {
		t.Fatalf("diagnostics=%v", d)
	}
}

func TestEnrichIndexStatusDiagnosticsUnicodeHint(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only diagnostic")
	}
	root := t.TempDir()
	settings := &config.Settings{
		WorkspaceRoot: root,
		IndexDir:      filepath.Join(root, "База", "index"),
	}
	d := indexStatusDiagnostics(settings)
	enrichIndexStatusDiagnostics(d, settings, 3, 77, 12, true, 0, false)
	if d["non_ascii_index_dir"] != true {
		t.Fatalf("diagnostics=%v", d)
	}
	if d["unicode_paths_supported"] != true {
		t.Fatalf("diagnostics=%v", d)
	}
	if _, ok := d["unicode_index_path_suspected"]; ok {
		t.Fatalf("unexpected unicode_index_path_suspected when zvec open ok: %v", d)
	}
	if d["hint"] == "" {
		t.Fatal("expected hint for files_failed")
	}
}

func TestEnrichIndexStatusDiagnosticsUnicodeZvecFailure(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only diagnostic")
	}
	root := t.TempDir()
	settings := &config.Settings{
		WorkspaceRoot: root,
		IndexDir:      filepath.Join(root, "База", "index"),
	}
	d := indexStatusDiagnostics(settings)
	enrichIndexStatusDiagnostics(d, settings, 0, 0, 0, false, 0, false)
	if d["unicode_index_path_suspected"] != true {
		t.Fatalf("diagnostics=%v", d)
	}
}

func TestEnrichIndexStatusDiagnosticsCloudDrive(t *testing.T) {
	root := t.TempDir()
	settings := &config.Settings{
		WorkspaceRoot: filepath.Join(root, "GDrive", "База знаний"),
		IndexDir:      filepath.Join(root, "GDrive", "База знаний", ".mcp-semantic-search-zvec-go", "data", "index"),
	}
	d := indexStatusDiagnostics(settings)
	enrichIndexStatusDiagnostics(d, settings, 1, 10, 10, true, 0, false)
	if d["synced_cloud_drive_suspected"] != true {
		t.Fatalf("diagnostics=%v", d)
	}
}

func TestEnrichIndexStatusDiagnosticsManifestEmpty(t *testing.T) {
	root := t.TempDir()
	settings := &config.Settings{
		WorkspaceRoot: root,
		IndexDir:      filepath.Join(root, "index"),
	}
	d := indexStatusDiagnostics(settings)
	enrichIndexStatusDiagnostics(d, settings, 0, 0, 100, false, 0, false)
	if d["zvec_manifest_empty_suspected"] != true {
		t.Fatalf("diagnostics=%v", d)
	}
	hint, _ := d["hint"].(string)
	if !strings.Contains(hint, "zvec is empty") {
		t.Fatalf("hint=%q", hint)
	}
}

func TestEnrichIndexStatusDiagnosticsManifestMismatch(t *testing.T) {
	root := t.TempDir()
	settings := &config.Settings{
		WorkspaceRoot: root,
		IndexDir:      filepath.Join(root, "index"),
	}
	d := indexStatusDiagnostics(settings)
	enrichIndexStatusDiagnostics(d, settings, 0, 77, 12, true, 0, false)
	if d["zvec_manifest_mismatch_suspected"] != true {
		t.Fatalf("diagnostics=%v", d)
	}
}

func TestEnrichIndexStatusDiagnosticsIdentityMismatch(t *testing.T) {
	root := t.TempDir()
	settings := &config.Settings{
		WorkspaceRoot: root,
		IndexDir:      filepath.Join(root, "index"),
	}
	d := indexStatusDiagnostics(settings)
	enrichIndexStatusDiagnostics(d, settings, 0, 0, 100, false, 0, true)
	hint, _ := d["hint"].(string)
	if !strings.Contains(hint, "active_profile or embedding dimensions changed") {
		t.Fatalf("hint=%q", hint)
	}
}
