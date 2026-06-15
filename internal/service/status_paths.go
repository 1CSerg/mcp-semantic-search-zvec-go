package service

import (
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
	return map[string]any{
		"log_dir": statusRelativePath(settings.WorkspaceRoot, settings.LogsDir()),
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

func enrichIndexStatusDiagnostics(
	diag map[string]any,
	settings *config.Settings,
	filesFailed int,
	docCount int,
	manifestChunks int,
) {
	if runtime.GOOS == "windows" && pathContainsNonASCII(settings.IndexDir) {
		diag["non_ascii_index_dir"] = true
	}
	if filesFailed > 0 && runtime.GOOS == "windows" && pathContainsNonASCII(settings.IndexDir) {
		diag["unicode_index_path_suspected"] = true
		if _, ok := diag["hint"]; !ok {
			diag["hint"] = "INDEX_DIR contains non-ASCII characters on Windows; set INDEX_DIR to an ASCII path (or upgrade zvec-go with Unicode path fix) and run reindex with force=true"
		}
	}
	if docCount > 0 && manifestChunks > 0 && docCount > manifestChunks*2 {
		diag["zvec_manifest_mismatch_suspected"] = true
		if _, ok := diag["hint"]; !ok {
			diag["hint"] = "zvec doc count exceeds manifest chunks; run reindex with force=true"
		}
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
