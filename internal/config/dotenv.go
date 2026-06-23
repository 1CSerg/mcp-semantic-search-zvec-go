package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotEnv reads KEY=VALUE pairs from candidate paths (merged in order).
// Does not override variables already set in the process environment.
// Missing files are skipped without error.
func LoadDotEnv(paths ...string) error {
	parsed, err := ParseDotEnv(paths...)
	if err != nil {
		return err
	}
	for key, value := range parsed {
		if !isProcessEnvKey(key) || os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

// ParseDotEnv reads KEY=VALUE pairs from all existing paths into a map.
// Later paths override earlier keys. Empty files are skipped.
// Does not mutate the process environment.
func ParseDotEnv(paths ...string) (map[string]string, error) {
	out := make(map[string]string)
	for _, path := range paths {
		if path == "" {
			continue
		}
		parsed, err := parseDotEnvFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		if len(parsed) == 0 {
			continue
		}
		for k, v := range parsed {
			out[k] = v
		}
	}
	return out, nil
}

func loadDotEnvCandidates(workspace, configPath string) error {
	var paths []string
	if p := os.Getenv("ENV_PATH"); p != "" {
		paths = append(paths, p)
	}
	if configPath != "" {
		paths = append(paths, filepath.Join(filepath.Dir(configPath), ".env"))
	}
	paths = append(paths, filepath.Join(workspace, DefaultInstallDirName, ".env"))

	parsed, err := ParseDotEnv(paths...)
	if err != nil {
		return err
	}
	// Only non-secret keys explicitly allowlisted go into the process environment
	// for os.Getenv overrides. All other keys (including custom api_key_env names)
	// stay in the private secrets map loaded by LoadWithOptions.
	for key, value := range parsed {
		if !isProcessEnvKey(key) || os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

// isProcessEnvKey reports env keys safe to export into the process environment.
func isProcessEnvKey(key string) bool {
	switch strings.ToUpper(strings.TrimSpace(key)) {
	case "HTTP_ADDR", "AUTO_INDEX_ON_START", "WORKSPACE_ROOT", "INDEX_DIR", "CONFIG_PATH", "ENV_PATH",
		"SEARCH_SLOW_THRESHOLD_SECONDS", "SEARCH_DEGRADE_RATIO", "SEARCH_STATS_WINDOW",
		"INDEXING_STALL_SECONDS", "INDEXING_MAX_FILE_BYTES", "INDEXING_STREAM_CHUNK_THRESHOLD_BYTES",
		"INDEXING_MAX_LINE_BYTES", "FILE_WATCHER_ENABLED", "FILE_WATCHER_BACKEND",
		"FILE_WATCHER_POLL_INTERVAL_SECONDS", "MCP_LOG_LEVEL", "MCP_LOG_VERBOSE",
		"MCP_LOG_MAX_BYTES", "MCP_LOG_BACKUP_COUNT", "PATH_CONTAINMENT":
		return true
	default:
		return false
	}
}

const (
	maxDotEnvFileSize = 64 << 10 // 64 KiB
	maxDotEnvLineSize = 64 << 10
)

func parseDotEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	if info, err := f.Stat(); err == nil && info.Size() > maxDotEnvFileSize {
		return nil, fmt.Errorf(".env too large: %d bytes (max %d)", info.Size(), maxDotEnvFileSize)
	}

	out := make(map[string]string)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), maxDotEnvLineSize)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		key, value, err := parseDotEnvLine(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if key == "" {
			continue
		}
		out[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseDotEnvLine(line string) (key, value string, err error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", nil
	}
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}

	k, v, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", fmt.Errorf("invalid line %q", line)
	}
	k = strings.TrimSpace(k)
	if k == "" {
		return "", "", fmt.Errorf("empty key")
	}
	v = strings.TrimSpace(unquoteDotEnvValue(v))
	return k, v, nil
}

func unquoteDotEnvValue(v string) string {
	if len(v) < 2 {
		return v
	}
	if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}
