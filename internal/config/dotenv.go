package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotEnv reads KEY=VALUE pairs from the first existing path.
// Does not override variables already set in the process environment.
// Missing files are skipped without error.
func LoadDotEnv(paths ...string) error {
	parsed, err := ParseDotEnv(paths...)
	if err != nil {
		return err
	}
	for key, value := range parsed {
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

// ParseDotEnv reads KEY=VALUE pairs from the first existing path into a map.
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
		for k, v := range parsed {
			out[k] = v
		}
		return out, nil
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
	// Non-secret keys (HTTP_ADDR, AUTO_INDEX_ON_START, ...) go into the process
	// environment for os.Getenv overrides. Secrets (API tokens, embedding keys)
	// are deliberately NOT exported — LoadWithOptions reads them from a private
	// map so they never appear in /proc/<pid>/environ or child processes.
	for key, value := range parsed {
		if isSecretEnvKey(key) || os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

// isSecretEnvKey reports whether an env key likely holds a credential.
func isSecretEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "APIKEY", "CREDENTIAL"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
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
	defer f.Close()

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
