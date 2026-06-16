package crash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

// Report describes a fatal process exit.
type Report struct {
	Timestamp     string `json:"timestamp"`
	Version       string `json:"version"`
	WorkspaceRoot string `json:"workspace_root,omitempty"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	Error         string `json:"error"`
	Stack         string `json:"stack,omitempty"`
}

// WriteOptions configures crash report redaction.
type WriteOptions struct {
	RedactPaths   bool
	WorkspaceRoot string
	WorkspaceID   string
}

// RedactPathsEnabled reports whether absolute paths should be redacted in crash reports.
// Default is true unless MCP_CRASH_REDACT_PATHS is explicitly false/off/0.
func RedactPathsEnabled() bool {
	v := strings.TrimSpace(os.Getenv("MCP_CRASH_REDACT_PATHS"))
	if v == "" {
		return true
	}
	return config.ParseBoolEnv(v)
}

// Write saves last_crash.json under logDir.
func Write(logDir, version, workspaceRoot string, fatal any) error {
	return WriteWithOptions(logDir, version, fatal, WriteOptions{
		RedactPaths:   RedactPathsEnabled(),
		WorkspaceRoot: workspaceRoot,
	})
}

// WriteWithOptions saves last_crash.json with optional path redaction.
func WriteWithOptions(logDir, version string, fatal any, opts WriteOptions) error {
	if fatal == nil {
		return nil
	}
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return err
	}
	var msg string
	switch v := fatal.(type) {
	case error:
		msg = v.Error()
	default:
		msg = fmt.Sprint(v)
	}
	stack := string(debug.Stack())
	workspaceRoot := opts.WorkspaceRoot
	if opts.RedactPaths {
		roots := redactRoots(opts.WorkspaceRoot)
		stack = SanitizeStack(stack, roots...)
		msg = SanitizeStack(msg, roots...)
		workspaceRoot = RedactPath(workspaceRoot, roots...)
	}
	report := Report{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Version:       version,
		WorkspaceRoot: workspaceRoot,
		WorkspaceID:   opts.WorkspaceID,
		Error:         msg,
		Stack:         stack,
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(logDir, "last_crash.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(logDir, "last_crash.json"))
}

// SanitizeStack replaces known absolute path prefixes with placeholders.
func SanitizeStack(stack string, roots ...string) string {
	out := stack
	for _, root := range roots {
		if root == "" {
			continue
		}
		out = strings.ReplaceAll(out, root, "<redacted>")
		out = strings.ReplaceAll(out, filepath.ToSlash(root), "<redacted>")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = strings.ReplaceAll(out, home, "<home>")
		out = strings.ReplaceAll(out, filepath.ToSlash(home), "<home>")
	}
	return out
}

// RedactPath replaces a single path with a placeholder when redaction is enabled.
func RedactPath(path string, roots ...string) string {
	if path == "" {
		return ""
	}
	for _, root := range roots {
		if root != "" && strings.HasPrefix(filepath.Clean(path), filepath.Clean(root)) {
			return "<redacted>"
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if strings.HasPrefix(filepath.Clean(path), filepath.Clean(home)) {
			return "<home>"
		}
	}
	return path
}

func redactRoots(workspaceRoot string) []string {
	var roots []string
	if workspaceRoot != "" {
		roots = append(roots, workspaceRoot)
	}
	if v := os.Getenv("GOPATH"); v != "" {
		roots = append(roots, v)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, home)
	}
	return roots
}

// ProxyLogDir returns the log directory for stdio-proxy crash reports.
func ProxyLogDir() string {
	if v := strings.TrimSpace(os.Getenv("MCP_PROXY_LOG_DIR")); v != "" {
		return v
	}
	return filepath.Join(os.TempDir(), "mcp-semantic-search-zvec-go-proxy")
}

// DaemonLogDir returns the log directory for shared daemon crash reports.
func DaemonLogDir() string {
	if v := strings.TrimSpace(os.Getenv("MCP_DAEMON_LOG_DIR")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("LOG_DIR")); v != "" {
		return v
	}
	return filepath.Join(".", "logs")
}
