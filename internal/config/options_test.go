package config

import (
	"os"
	"path/filepath"
	"strings"
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
	settings, err := LoadWorkspaceFromSpec("my-app", dir, filepath.Join(dir, "idx"), path, LoadOptions{})
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

func TestWarnPlaintextAPIKeys(t *testing.T) {
	app := AppConfig{
		Profiles: map[string]EmbeddingProfile{
			"leaky": {Provider: "openai_compatible", APIKey: "hardcoded"},
			"safe":  {Provider: "openai_compatible", APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
	warnPlaintextAPIKeys(&app, "/tmp/config.yaml")
}

func TestDotEnvCandidatePathsWithEnvPath(t *testing.T) {
	paths := dotEnvCandidatePaths("/ws", "/ws/config.yaml", "/custom/.env")
	if len(paths) != 3 {
		t.Fatalf("paths=%v", paths)
	}
	if paths[0] != filepath.Join(filepath.Dir("/ws/config.yaml"), ".env") {
		t.Fatalf("paths[0]=%q", paths[0])
	}
	if paths[len(paths)-1] != "/custom/.env" {
		t.Fatalf("paths last=%q", paths[len(paths)-1])
	}
}

func TestCloneSecretsEmpty(t *testing.T) {
	if cloneSecrets(nil) != nil {
		t.Fatal("expected nil for nil input")
	}
	if cloneSecrets(map[string]string{}) != nil {
		t.Fatal("expected nil for empty map")
	}
}

func TestMergeDotEnvIntoMapParseError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, ".env")
	if err := os.WriteFile(bad, []byte("INVALID LINE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := mergeDotEnvIntoMap(map[string]string{}, []string{bad})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestApplyProfileSecretsSkipsPlaintextKey(t *testing.T) {
	app := AppConfig{
		Profiles: map[string]EmbeddingProfile{
			"p": {APIKey: "set", APIKeyEnv: "OTHER"},
		},
	}
	applyProfileSecrets(&app, map[string]string{"OTHER": "from-env"}, false)
	if app.Profiles["p"].APIKey != "set" {
		t.Fatal("should not overwrite plaintext api_key")
	}
}

func TestLookupSecretFromMapAndEnv(t *testing.T) {
	if got := lookupSecret(map[string]string{"K": "from-map"}, "K", false); got != "from-map" {
		t.Fatalf("got=%q", got)
	}
	t.Setenv("ENV_KEY", "from-env")
	if got := lookupSecret(nil, "ENV_KEY", true); got != "from-env" {
		t.Fatalf("got=%q", got)
	}
	if got := lookupSecret(map[string]string{}, "MISSING", true); got != "" {
		t.Fatalf("got=%q", got)
	}
}

func TestLoadWorkspaceFromSpecRequiresID(t *testing.T) {
	_, err := LoadWorkspaceFromSpec("", t.TempDir(), "", "", LoadOptions{})
	if err == nil || !strings.Contains(err.Error(), "workspace id is required") {
		t.Fatalf("err=%v", err)
	}
}
