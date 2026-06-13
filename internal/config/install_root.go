package config

import (
	"os"
	"path/filepath"
	"strings"
)

// WorkspaceRootMarkerFile is a legacy marker next to the binary (old AppData staging installs).
const WorkspaceRootMarkerFile = "workspace-root.txt"

// ResolveWorkspaceRoot returns WORKSPACE_ROOT from env, install marker, or cwd.
func ResolveWorkspaceRoot(envValue string) string {
	if v := strings.TrimSpace(envValue); v != "" {
		return mustAbs(v)
	}
	if root, err := WorkspaceRootFromExecutable(); err == nil && root != "" {
		return root
	}
	return mustAbs(getwd())
}

// WorkspaceRootFromExecutable reads workspace-root.txt next to the running binary.
func WorkspaceRootFromExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	return ReadWorkspaceRootMarker(filepath.Dir(exe))
}

// ReadWorkspaceRootMarker loads the UTF-8 workspace path from dir/workspace-root.txt.
func ReadWorkspaceRootMarker(dir string) (string, error) {
	path := filepath.Join(dir, WorkspaceRootMarkerFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(data))
	if root == "" {
		return "", os.ErrInvalid
	}
	return filepath.Abs(root)
}
