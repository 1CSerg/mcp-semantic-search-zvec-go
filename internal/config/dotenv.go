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
	return LoadDotEnv(paths...)
}

func parseDotEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := make(map[string]string)
	scanner := bufio.NewScanner(f)
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
