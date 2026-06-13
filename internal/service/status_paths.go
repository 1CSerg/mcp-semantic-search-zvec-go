package service

import (
	"path/filepath"
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
