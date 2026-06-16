package config

import "testing"

func TestLoadWithOptionsHTTPAddrOverride(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	settings, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot: dir,
		ConfigPath:    path,
		HTTPAddr:      ":7777",
		UseProcessEnv: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.HTTPAddr != ":7777" {
		t.Fatalf("http_addr=%q", settings.HTTPAddr)
	}
}

func TestLoadWithOptionsGitHubRepoDefault(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	settings, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot: dir,
		ConfigPath:    path,
		UseProcessEnv: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.GitHubRepo == "" {
		t.Fatal("expected default github repo")
	}
}

func TestLoadWorkspaceFromSpecEmptyID(t *testing.T) {
	if _, err := LoadWorkspaceFromSpec("", t.TempDir(), "", "", LoadOptions{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadWithOptionsAPITokenFromSecrets(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	settings, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot: dir,
		ConfigPath:    path,
		Secrets:       map[string]string{"API_TOKEN": "from-map"},
		UseProcessEnv: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.APIToken != "from-map" {
		t.Fatalf("api_token=%q", settings.APIToken)
	}
}

func TestLoadWithOptionsAutoIndex(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	auto := true
	settings, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot:    dir,
		ConfigPath:       path,
		AutoIndexOnStart: &auto,
		UseProcessEnv:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !settings.AutoIndexOnStart {
		t.Fatal("expected auto index")
	}
}
