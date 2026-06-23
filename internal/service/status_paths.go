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
	return config.PathIsSyncedCloudDrive(path)
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

func setDuplicateStdioDiagnostic(diag map[string]any) {
	diag["duplicate_stdio_suspected"] = true
	setDiagnosticHint(diag, "Restart Cursor or kill extra mcp-semantic-search-zvec-go processes for this workspace")
}

func enrichIndexStatusDiagnostics(
	diag map[string]any,
	settings *config.Settings,
	filesFailed int,
	docCount int,
	manifestChunks int,
	zvecOpenOK bool,
	skippedScanPaths int,
	identityMismatch bool,
) {
	if identityMismatch {
		setDiagnosticHint(diag, "active_profile or embedding dimensions changed; run reindex with force=true")
	}

	if runtime.GOOS == "windows" && pathContainsNonASCII(settings.IndexDir) {
		diag["non_ascii_index_dir"] = true
		if zvecOpenOK {
			diag["unicode_paths_supported"] = true
		} else {
			setDiagnosticHint(diag, "Can't open lock file on a non-ASCII INDEX_DIR is usually duplicate MCP stdio processes or a stale zvec LOCK, not path encoding; restart Cursor and check diagnostics.duplicate_stdio_suspected; zvec stays under INDEX_DIR/zvec/; ASCII INDEX_DIR is a last-resort fallback")
		}
	}

	if filesFailed > 0 {
		setDiagnosticHint(diag, fmt.Sprintf("%d file(s) failed indexing; grep \"index file skipped\" in diagnostics.log_file; paths in indexing.failed_files", filesFailed))
		if pathIsSyncedCloudDrive(settings.IndexDir) || pathIsSyncedCloudDrive(settings.WorkspaceRoot) {
			diag["synced_cloud_drive_suspected"] = true
			setDiagnosticHint(diag, "cloud-synced folders (Google Drive/YandexDisk) can cause transient zvec errors during indexing")
		}
	}

	if skippedScanPaths > 0 {
		diag["scan_paths_skipped"] = skippedScanPaths
		setDiagnosticHint(diag, fmt.Sprintf("%d path(s) skipped during workspace scan (permission or unreadable); see indexing.skipped_paths", skippedScanPaths))
	}

	if docCount > 0 && manifestChunks > 0 && docCount > manifestChunks*2 {
		diag["zvec_manifest_mismatch_suspected"] = true
		setDiagnosticHint(diag, "zvec doc count exceeds manifest chunks; run reindex with force=true")
	}
	if manifestChunks > 0 && docCount == 0 {
		diag["zvec_manifest_empty_suspected"] = true
		setDiagnosticHint(diag, "manifest has chunks but zvec is empty; run reindex with force=true")
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
