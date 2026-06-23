package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if got := os.Getenv("DASHSCOPE_API_KEY"); got != "" {
		t.Fatalf("DASHSCOPE_API_KEY = %q, want empty (secrets not exported)", got)
	}
	if got := os.Getenv("QUOTED"); got != "" {
		t.Fatalf("QUOTED = %q, want empty (non-allowlisted keys not exported)", got)
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

func TestLoadDotEnvCandidatesWithEnvPath(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "custom.env")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(envPath, []byte("HTTP_ADDR=:7777\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("x: 1"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ENV_PATH", envPath)
	t.Setenv("HTTP_ADDR", "")

	if err := loadDotEnvCandidates(dir, configPath); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("HTTP_ADDR"); got != ":7777" {
		t.Fatalf("HTTP_ADDR = %q, want :7777", got)
	}
}

func TestIsSecretEnvKey(t *testing.T) {
	isSecretEnvKey := func(key string) bool {
		upper := strings.ToUpper(key)
		for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "APIKEY", "CREDENTIAL"} {
			if strings.Contains(upper, marker) {
				return true
			}
		}
		return false
	}
	secrets := []string{"API_TOKEN", "OPENAI_API_KEY", "DB_PASSWORD", "MY_SECRET", "X_CREDENTIAL"}
	for _, k := range secrets {
		if !isSecretEnvKey(k) {
			t.Fatalf("expected %q to be secret", k)
		}
	}
	if isSecretEnvKey("HTTP_ADDR") {
		t.Fatal("HTTP_ADDR should not be secret")
	}
}

func TestIsProcessEnvKey(t *testing.T) {
	allowed := []string{"HTTP_ADDR", "AUTO_INDEX_ON_START", "INDEX_DIR", "MCP_LOG_LEVEL"}
	for _, k := range allowed {
		if !isProcessEnvKey(k) {
			t.Fatalf("expected %q to be process env key", k)
		}
	}
	denied := []string{"ROUTERAI_API_KEY", "MY_CUSTOM_KEY", "DASHSCOPE_API_KEY"}
	for _, k := range denied {
		if isProcessEnvKey(k) {
			t.Fatalf("expected %q to be denied", k)
		}
	}
}

func TestParseDotEnvMergesSkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.env")
	real := filepath.Join(dir, "real.env")
	if err := os.WriteFile(empty, []byte("# only comments\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("HTTP_ADDR=:8081\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ParseDotEnv(empty, real)
	if err != nil {
		t.Fatal(err)
	}
	if got["HTTP_ADDR"] != ":8081" {
		t.Fatalf("ParseDotEnv merge = %v", got)
	}
}

func TestParseDotEnvEmptyPathSkipped(t *testing.T) {
	got, err := ParseDotEnv("", filepath.Join(t.TempDir(), "missing.env"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestParseDotEnvFileTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	data := make([]byte, maxDotEnvFileSize+1)
	for i := range data {
		data[i] = 'x'
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseDotEnvFile(path); err == nil {
		t.Fatal("expected file too large error")
	}
}

func TestParseDotEnvEmptyKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseDotEnv(path); err == nil {
		t.Fatal("expected empty key error")
	}
}

func TestParseDotEnvSingleQuoted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("HTTP_ADDR=':8080'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ParseDotEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["HTTP_ADDR"] != ":8080" {
		t.Fatalf("HTTP_ADDR=%q", got["HTTP_ADDR"])
	}
}
