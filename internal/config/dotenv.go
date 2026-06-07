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
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := loadDotEnvFile(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("load %s: %w", path, err)
		}
		return nil
	}
	return nil
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

func loadDotEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if err := applyDotEnvLine(scanner.Text()); err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
	}
	return scanner.Err()
}

func applyDotEnvLine(line string) error {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}

	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return fmt.Errorf("invalid line %q", line)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("empty key")
	}
	value = strings.TrimSpace(unquoteDotEnvValue(value))

	if os.Getenv(key) != "" {
		return nil
	}
	return os.Setenv(key, value)
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
