package config

import (
	"os"
	"path/filepath"
	"testing"
)

const minimalConfigYAML = `active_profile: test
profiles:
  test:
    provider: openai_compatible
    model: test-model
    dimensions: 384
    base_url: http://127.0.0.1:9/v1
server:
  http_addr: ":9090"
`

func writeTestConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(minimalConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAppConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)

	app, err := LoadAppConfig(path)
	if err != nil {
		t.Fatalf("LoadAppConfig: %v", err)
	}
	if app.ActiveProfile != "test" {
		t.Fatalf("active_profile=%q", app.ActiveProfile)
	}
	if app.Indexing.LockStaleSeconds != DefaultLockStaleSeconds {
		t.Fatalf("lock_stale_seconds=%v", app.Indexing.LockStaleSeconds)
	}
	if app.Profiles["test"].BatchSize != 32 {
		t.Fatalf("batch_size=%d", app.Profiles["test"].BatchSize)
	}
}

func TestLoadAppConfigMissingFile(t *testing.T) {
	_, err := LoadAppConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	indexDir := filepath.Join(dir, "data", "index")

	t.Setenv("WORKSPACE_ROOT", dir)
	t.Setenv("CONFIG_PATH", path)
	t.Setenv("INDEX_DIR", indexDir)
	t.Setenv("AUTO_INDEX_ON_START", "true")
	t.Setenv("HTTP_ADDR", ":8081")
	t.Setenv("API_TOKEN", "secret")

	settings, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.WorkspaceRoot != dir {
		t.Fatalf("workspace=%q", settings.WorkspaceRoot)
	}
	if settings.HTTPAddr != ":9090" {
		t.Fatalf("http_addr=%q", settings.HTTPAddr)
	}
	if !settings.AutoIndexOnStart {
		t.Fatal("expected auto index on start")
	}
	if settings.APIToken != "secret" {
		t.Fatalf("api_token=%q", settings.APIToken)
	}
}

func TestActiveProfile(t *testing.T) {
	settings := &Settings{
		App: AppConfig{
			ActiveProfile: "ok",
			Profiles: map[string]EmbeddingProfile{
				"ok": {Provider: "openai_compatible"},
			},
		},
	}
	if _, err := settings.ActiveProfile(); err != nil {
		t.Fatalf("ActiveProfile: %v", err)
	}

	settings.App.ActiveProfile = ""
	if _, err := settings.ActiveProfile(); err == nil {
		t.Fatal("expected error for empty profile")
	}

	settings.App.ActiveProfile = "missing"
	if _, err := settings.ActiveProfile(); err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestLogsDir(t *testing.T) {
	settings := &Settings{IndexDir: filepath.Join("/workspace", DefaultInstallDirName, DefaultIndexSubdir)}
	want := filepath.Join("/workspace", DefaultInstallDirName, "logs")
	if got := settings.LogsDir(); got != want {
		t.Fatalf("LogsDir=%q want %q", got, want)
	}
}

func TestParseBoolEnv(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"true", true},
		{"YES", true},
		{"on", true},
		{"false", false},
		{"", false},
	} {
		if got := ParseBoolEnv(tc.in); got != tc.want {
			t.Fatalf("ParseBoolEnv(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseIntEnv(t *testing.T) {
	t.Setenv("TEST_INT", "42")
	if got := ParseIntEnv("TEST_INT", 7); got != 42 {
		t.Fatalf("got %d", got)
	}
	if got := ParseIntEnv("MISSING_INT", 7); got != 7 {
		t.Fatalf("got %d", got)
	}
	t.Setenv("TEST_INT", "nope")
	if got := ParseIntEnv("TEST_INT", 7); got != 7 {
		t.Fatalf("got %d", got)
	}
}
