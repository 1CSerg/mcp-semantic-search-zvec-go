package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `# comment
ROUTERAI_API_KEY=from-file
QUOTED="hello world"
EMPTY=

export DASHSCOPE_API_KEY=exported
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ROUTERAI_API_KEY", "preset")
	t.Setenv("DASHSCOPE_API_KEY", "")
	t.Setenv("QUOTED", "")
	t.Setenv("EMPTY", "")
	t.Setenv("NEW_VAR", "")

	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv("ROUTERAI_API_KEY"); got != "preset" {
		t.Fatalf("ROUTERAI_API_KEY = %q, want preset (no override)", got)
	}
	if got := os.Getenv("DASHSCOPE_API_KEY"); got != "exported" {
		t.Fatalf("DASHSCOPE_API_KEY = %q, want exported", got)
	}
	if got := os.Getenv("QUOTED"); got != "hello world" {
		t.Fatalf("QUOTED = %q", got)
	}
	if got := os.Getenv("EMPTY"); got != "" {
		t.Fatalf("EMPTY = %q, want empty", got)
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatal(err)
	}
}

func TestParseDotEnvInvalidLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("INVALID LINE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseDotEnv(path); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadDotEnvCandidates(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(configPath, []byte("x: 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("API_TOKEN=secret-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ENV_PATH", "")
	t.Setenv("API_TOKEN", "")

	if err := loadDotEnvCandidates(dir, configPath); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("API_TOKEN"); got != "secret-token" {
		t.Fatalf("API_TOKEN = %q", got)
	}
}
