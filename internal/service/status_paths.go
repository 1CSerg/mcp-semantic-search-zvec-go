package service

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

// statusRelativePath returns path for index_status JSON relative to workspaceRoot.
// workspace_root itself is the only absolute path in the payload.
func statusRelativePath(workspaceRoot, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		root = filepath.Clean(workspaceRoot)
	}
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(root, path)
	}
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	return filepath.ToSlash(rel)
}

func indexStatusDiagnostics(settings *config.Settings) map[string]any {
	logDir := settings.LogsDir()
	return map[string]any{
		"log_dir":  statusRelativePath(settings.WorkspaceRoot, logDir),
		"log_file": statusRelativePath(settings.WorkspaceRoot, filepath.Join(logDir, "server.log")),
	}
}

func pathContainsNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}

func pathIsSyncedCloudDrive(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	markers := []string{
		"gdrive",
		"google drive",
		"googledrive",
		"yandexdisk",
		"yandex disk",
		"onedrive",
		"dropbox",
		"icloud",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func setDiagnosticHint(diag map[string]any, hint string) {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return
	}
	if existing, ok := diag["hint"].(string); ok && strings.TrimSpace(existing) != "" {
		diag["hint"] = strings.TrimSpace(existing + "; " + hint)
		return
	}
	diag["hint"] = hint
}

func enrichIndexStatusDiagnostics(
	diag map[string]any,
	settings *config.Settings,
	filesFailed int,
	docCount int,
	manifestChunks int,
	zvecOpenOK bool,
) {
	if runtime.GOOS == "windows" && pathContainsNonASCII(settings.IndexDir) {
		diag["non_ascii_index_dir"] = true
		diag["unicode_paths_supported"] = true
		if !zvecOpenOK {
			diag["unicode_index_path_suspected"] = true
			setDiagnosticHint(diag, "zvec failed to open index on a non-ASCII INDEX_DIR; set INDEX_DIR to an ASCII path and run reindex with force=true")
		}
	}

	if filesFailed > 0 {
		setDiagnosticHint(diag, fmt.Sprintf("%d file(s) failed indexing; grep \"index file skipped\" in diagnostics.log_file; paths in indexing.failed_files", filesFailed))
		if pathIsSyncedCloudDrive(settings.IndexDir) || pathIsSyncedCloudDrive(settings.WorkspaceRoot) {
			diag["synced_cloud_drive_suspected"] = true
			setDiagnosticHint(diag, "cloud-synced folders (Google Drive/YandexDisk) can cause transient zvec errors during indexing")
		}
	}

	if docCount > 0 && manifestChunks > 0 && docCount > manifestChunks*2 {
		diag["zvec_manifest_mismatch_suspected"] = true
		setDiagnosticHint(diag, "zvec doc count exceeds manifest chunks; run reindex with force=true")
	}
	if msg, ok := diag["hint"].(string); ok {
		diag["hint"] = strings.TrimSpace(msg)
	}
}

func relativeIndexingMap(workspaceRoot string, idx map[string]any) map[string]any {
	if idx == nil {
		return idx
	}
	if cf, ok := idx["current_file"].(string); ok && cf != "" {
		out := make(map[string]any, len(idx))
		for k, v := range idx {
			out[k] = v
		}
		out["current_file"] = statusRelativePath(workspaceRoot, cf)
		return out
	}
	return idx
}
