package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWithOptionsExplicitPaths(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	indexDir := filepath.Join(dir, "custom", "index")

	settings, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot: dir,
		WorkspaceID:   "ws-test",
		IndexDir:      indexDir,
		ConfigPath:    path,
		UseProcessEnv: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.WorkspaceID != "ws-test" {
		t.Fatalf("workspace_id=%q", settings.WorkspaceID)
	}
	wantIndex := filepath.Join(dir, "custom", "index")
	if settings.IndexDir != wantIndex {
		t.Fatalf("index_dir=%q want %q", settings.IndexDir, wantIndex)
	}
}

func TestLoadWithOptionsPerWorkspaceEnvNoGlobalLeak(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)
	configPath = filepath.Join(dir, "config.yaml")
	profileYAML := `active_profile: secret_profile
profiles:
  secret_profile:
    provider: openai_compatible
    model: m
    dimensions: 384
    base_url: http://127.0.0.1:9/v1
    api_key_env: ROUTERAI_API_KEY
`
	if err := os.WriteFile(configPath, []byte(profileYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("ROUTERAI_API_KEY=workspace-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ROUTERAI_API_KEY", "")

	settings, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot: dir,
		ConfigPath:    configPath,
		UseProcessEnv: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := settings.ActiveProfile()
	if err != nil {
		t.Fatal(err)
	}
	if profile.APIKey != "workspace-secret" {
		t.Fatalf("api_key=%q want workspace-secret", profile.APIKey)
	}
	if got := os.Getenv("ROUTERAI_API_KEY"); got != "" {
		t.Fatalf("process env leaked: ROUTERAI_API_KEY=%q", got)
	}
}

func TestLoadWorkspaceFromSpec(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	settings, err := LoadWorkspaceFromSpec("my-app", dir, filepath.Join(dir, "idx"), path)
	if err != nil {
		t.Fatal(err)
	}
	if settings.WorkspaceID != "my-app" {
		t.Fatalf("workspace_id=%q", settings.WorkspaceID)
	}
}

func TestParseDotEnvMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("KEY=value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ParseDotEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if m["KEY"] != "value" {
		t.Fatalf("KEY=%q", m["KEY"])
	}
}
