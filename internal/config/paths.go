package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	envPathContainment = "MCP_PATH_CONTAINMENT"
	envGlobalDir       = "MCP_GLOBAL_DIR"

	// AppDirName is the user-level directory name for the global install (no dot prefix).
	AppDirName = "mcp-semantic-search-zvec-go"
)

// UserDataDir returns the user-level data directory for the global install.
// MCP_GLOBAL_DIR overrides the platform default (tests, portable installs); it
// must be an absolute path so derived paths do not depend on the working dir.
func UserDataDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv(envGlobalDir)); v != "" {
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("%s must be an absolute path, got %q", envGlobalDir, v)
		}
		return filepath.Clean(v), nil
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, AppDirName), nil
		}
		if base := os.Getenv("USERPROFILE"); base != "" {
			return filepath.Join(base, ".local", "share", AppDirName), nil
		}
		return "", fmt.Errorf("user data dir: neither LOCALAPPDATA nor USERPROFILE is set")
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, AppDirName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user data dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", AppDirName), nil
}

// GlobalConfigPath returns the global config.yaml path.
func GlobalConfigPath() (string, error) {
	dir, err := UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// GlobalEnvPath returns the global .env path.
func GlobalEnvPath() (string, error) {
	dir, err := UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".env"), nil
}

// ProjectsDir returns the directory holding per-project registration files.
func ProjectsDir() (string, error) {
	dir, err := UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "projects.d"), nil
}

// CollectionsDir returns the root directory for per-project index collections.
func CollectionsDir() (string, error) {
	dir, err := UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "data", "collections"), nil
}

// CollectionDir returns the index directory for one collection id.
func CollectionDir(collectionID string) (string, error) {
	if err := ValidateCollectionID(collectionID); err != nil {
		return "", err
	}
	dir, err := CollectionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, collectionID), nil
}

// GlobalLogsDir returns the global log directory.
func GlobalLogsDir() (string, error) {
	dir, err := UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "data", "logs"), nil
}

// GlobalBinDir returns the global binary directory.
func GlobalBinDir() (string, error) {
	dir, err := UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bin"), nil
}

// GlobalModelsDir returns the global embedding models directory.
func GlobalModelsDir() (string, error) {
	dir, err := UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "data", "models"), nil
}

// PathContainmentMode controls validation of resolved paths against workspace roots.
type PathContainmentMode string

const (
	PathContainmentStrict PathContainmentMode = "strict"
	PathContainmentWarn   PathContainmentMode = "warn"
	PathContainmentOff    PathContainmentMode = "off"
)

// ParsePathContainmentMode parses strict|warn|off; unknown values default to warn.
func ParsePathContainmentMode(raw string) PathContainmentMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(PathContainmentStrict):
		return PathContainmentStrict
	case string(PathContainmentOff):
		return PathContainmentOff
	default:
		return PathContainmentWarn
	}
}

// PathContainmentOptions configures a single path containment check.
type PathContainmentOptions struct {
	Mode         PathContainmentMode
	FieldName    string
	Path         string
	AllowedRoots []string
	Allowlist    []string
}

// IsPathUnderRoot reports whether path is equal to or contained within root.
func IsPathUnderRoot(root, path string) bool {
	rootNorm := normalizePathForContainment(resolvePathForContainment(root))
	pathNorm := normalizePathForContainment(resolvePathForContainment(path))
	if rootNorm == "" || pathNorm == "" {
		return false
	}
	if pathNorm == rootNorm {
		return true
	}
	rootWithSep := rootNorm + string(filepath.Separator)
	return strings.HasPrefix(pathNorm, rootWithSep)
}

// IsPathAllowed reports whether path is under any allowed root or allowlist entry.
func IsPathAllowed(path string, allowedRoots, allowlist []string) bool {
	for _, root := range allowedRoots {
		if IsPathUnderRoot(root, path) {
			return true
		}
	}
	for _, entry := range allowlist {
		if IsPathUnderRoot(entry, path) {
			return true
		}
	}
	return false
}

// ValidatePathContainment checks path against allowed roots and optional allowlist.
func ValidatePathContainment(opts PathContainmentOptions) error {
	if opts.Mode == PathContainmentOff || opts.Mode == "" {
		return nil
	}
	if IsPathAllowed(opts.Path, opts.AllowedRoots, opts.Allowlist) {
		return nil
	}
	msg := fmt.Sprintf("%s %q is outside allowed workspace roots", opts.FieldName, opts.Path)
	if opts.Mode == PathContainmentWarn {
		slog.Warn("path containment violation", "field", opts.FieldName, "path", opts.Path, "allowed_roots", opts.AllowedRoots, "allowlist", opts.Allowlist)
		return nil
	}
	return fmt.Errorf("%s", msg)
}

// AbsPaths normalizes each path to an absolute cleaned path.
func AbsPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			return nil, fmt.Errorf("resolve path %q: %w", raw, err)
		}
		out = append(out, abs)
	}
	return out, nil
}

func resolvePathContainmentMode(opts LoadOptions) PathContainmentMode {
	if opts.PathContainment != "" {
		return opts.PathContainment
	}
	if opts.UseProcessEnv {
		if v := strings.TrimSpace(os.Getenv(envPathContainment)); v != "" {
			return ParsePathContainmentMode(v)
		}
	}
	return PathContainmentWarn
}

func validateSettingsPaths(workspace, indexDir, configPath string, mode PathContainmentMode, allowlist []string) error {
	roots := []string{workspace}
	if err := ValidatePathContainment(PathContainmentOptions{
		Mode:         mode,
		FieldName:    "INDEX_DIR",
		Path:         indexDir,
		AllowedRoots: roots,
		Allowlist:    allowlist,
	}); err != nil {
		return err
	}
	return ValidatePathContainment(PathContainmentOptions{
		Mode:         mode,
		FieldName:    "CONFIG_PATH",
		Path:         configPath,
		AllowedRoots: roots,
		Allowlist:    allowlist,
	})
}

func resolvePathForContainment(path string) string {
	path = filepath.Clean(path)
	// Resolve symlinks on the longest existing prefix so Windows short (8.3) and
	// long paths compare consistently when trailing segments do not exist yet.
	p := path
	for {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			if p == path {
				return resolved
			}
			suffix, err := filepath.Rel(p, path)
			if err != nil {
				return resolved
			}
			return filepath.Join(resolved, suffix)
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func normalizePathForContainment(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(strings.ReplaceAll(path, `/`, `\`))
	}
	return path
}

// NormalizeAbsolutePath canonicalizes a path for stable workspace identity (Windows drive letter case).
func NormalizeAbsolutePath(path string) string {
	return normalizePathForContainment(resolvePathForContainment(path))
}

// NormalizeWorkspaceID returns a canonical workspace_id; path-like ids are normalized like roots.
func NormalizeWorkspaceID(id, workspaceRoot string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return workspaceRoot
	}
	if strings.ContainsAny(id, `/\`) || strings.EqualFold(id, workspaceRoot) {
		if abs, err := filepath.Abs(id); err == nil {
			return NormalizeAbsolutePath(abs)
		}
	}
	return id
}

// cloudDriveMarkers are lowercase, separator-stripped folder-name prefixes for
// common cloud-sync clients. Matched against the start of each path segment
// (also separator-stripped) so "Yandex.Disk", "Yandex Disk", "Google Drive" and
// "OneDrive - Personal" are detected, while unrelated names like
// "my-dropbox-archive" are not.
var cloudDriveMarkers = []string{
	"gdrive",
	"googledrive",
	"yandexdisk",
	"onedrive",
	"dropbox",
	"icloud",
}

// PathIsSyncedCloudDrive reports common cloud-sync folder path markers.
func PathIsSyncedCloudDrive(path string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		norm := stripPathSeparators(seg)
		if norm == "" {
			continue
		}
		for _, marker := range cloudDriveMarkers {
			if strings.HasPrefix(norm, marker) {
				return true
			}
		}
	}
	return false
}

// stripPathSeparators lowercases a path segment and removes spaces, dots,
// underscores and hyphens so cosmetic variations collapse to a single form.
func stripPathSeparators(seg string) string {
	var b strings.Builder
	b.Grow(len(seg))
	for _, r := range strings.ToLower(seg) {
		switch r {
		case ' ', '.', '_', '-', '\t':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
