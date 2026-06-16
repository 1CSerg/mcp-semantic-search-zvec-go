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
	if err := os.WriteFile(envPath, []byte("API_TOKEN=secret-token\nHTTP_ADDR=:9999\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ENV_PATH", "")
	t.Setenv("API_TOKEN", "")
	t.Setenv("HTTP_ADDR", "")

	if err := loadDotEnvCandidates(dir, configPath); err != nil {
		t.Fatal(err)
	}
	// Secrets must NOT be exported to the process environment.
	if got := os.Getenv("API_TOKEN"); got != "" {
		t.Fatalf("API_TOKEN should not be exported to process env, got %q", got)
	}
	// Non-secret config keys are still exported for os.Getenv overrides.
	if got := os.Getenv("HTTP_ADDR"); got != ":9999" {
		t.Fatalf("HTTP_ADDR = %q, want :9999", got)
	}
}

func TestIsSecretEnvKey(t *testing.T) {
	secrets := []string{"API_TOKEN", "OPENAI_API_KEY", "DB_PASSWORD", "MY_SECRET", "X_CREDENTIAL"}
	for _, k := range secrets {
		if !isSecretEnvKey(k) {
			t.Fatalf("expected %q to be secret", k)
		}
	}
	nonSecrets := []string{"HTTP_ADDR", "WORKSPACE_ROOT", "AUTO_INDEX_ON_START", "INDEX_DIR"}
	for _, k := range nonSecrets {
		if isSecretEnvKey(k) {
			t.Fatalf("expected %q to be non-secret", k)
		}
	}
}
