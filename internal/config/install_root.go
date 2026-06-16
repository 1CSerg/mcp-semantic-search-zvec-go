package config

import (
	"fmt"
	"log/slog"
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
	return ReadWorkspaceRootMarkerValidated(filepath.Dir(exe))
}

// ReadWorkspaceRootMarker loads the UTF-8 workspace path from dir/workspace-root.txt.
func ReadWorkspaceRootMarker(dir string) (string, error) {
	return ReadWorkspaceRootMarkerValidated(dir)
}

// ReadWorkspaceRootMarkerValidated loads and validates workspace-root.txt.
func ReadWorkspaceRootMarkerValidated(dir string) (string, error) {
	path := filepath.Join(dir, WorkspaceRootMarkerFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(data))
	if root == "" {
		return "", os.ErrInvalid
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := ValidateWorkspaceRootMarker(abs); err != nil {
		return "", err
	}
	return abs, nil
}

// ValidateWorkspaceRootMarker checks that root exists and contains the MCP install tree.
func ValidateWorkspaceRootMarker(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return os.ErrInvalid
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("workspace root marker path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace root marker path is not a directory: %s", root)
	}
	installDir := filepath.Join(root, DefaultInstallDirName)
	if st, err := os.Stat(installDir); err != nil {
		return fmt.Errorf("workspace root missing install dir %q: %w", DefaultInstallDirName, err)
	} else if !st.IsDir() {
		return fmt.Errorf("workspace install path is not a directory: %s", installDir)
	}
	configPath := filepath.Join(installDir, "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		slog.Debug("workspace root marker: config.yaml not found in install dir", "path", configPath, "err", err)
	}
	return nil
}
