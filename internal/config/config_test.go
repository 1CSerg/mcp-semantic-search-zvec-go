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

func TestLoadRelativePaths(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)

	t.Setenv("WORKSPACE_ROOT", dir)
	t.Setenv("CONFIG_PATH", "custom/config.yaml")
	t.Setenv("INDEX_DIR", "custom/index")
	if err := os.MkdirAll(filepath.Join(dir, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "custom", "config.yaml"), []byte(minimalConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	wantIndex := filepath.Join(dir, "custom", "index")
	if settings.IndexDir != wantIndex {
		t.Fatalf("index_dir=%q want %q", settings.IndexDir, wantIndex)
	}
	wantConfig := filepath.Join(dir, "custom", "config.yaml")
	if settings.ConfigPath != wantConfig {
		t.Fatalf("config_path=%q want %q", settings.ConfigPath, wantConfig)
	}
	_ = path
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
	if settings.HTTPAddr != ":8081" {
		t.Fatalf("http_addr=%q want :8081 from HTTP_ADDR env", settings.HTTPAddr)
	}
	if !settings.AutoIndexOnStart {
		t.Fatal("expected auto index on start")
	}
	if settings.APIToken != "secret" {
		t.Fatalf("api_token=%q", settings.APIToken)
	}
}

func TestHTTPAddrDefaultLocal(t *testing.T) {
	dir := t.TempDir()
	minimalNoHTTP := `active_profile: test
profiles:
  test:
    provider: openai_compatible
    model: test-model
    dimensions: 384
    base_url: http://127.0.0.1:9/v1
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(minimalNoHTTP), 0o644); err != nil {
		t.Fatal(err)
	}
	indexDir := filepath.Join(dir, "data", "index")

	t.Setenv("WORKSPACE_ROOT", dir)
	t.Setenv("CONFIG_PATH", path)
	t.Setenv("INDEX_DIR", indexDir)
	t.Setenv("HTTP_ADDR", "")

	settings, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.HTTPAddr != DefaultHTTPAddrLocal {
		t.Fatalf("http_addr=%q want %q (per-project default)", settings.HTTPAddr, DefaultHTTPAddrLocal)
	}
}

func TestHTTPAddrConfigFallback(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	indexDir := filepath.Join(dir, "data", "index")

	t.Setenv("WORKSPACE_ROOT", dir)
	t.Setenv("CONFIG_PATH", path)
	t.Setenv("INDEX_DIR", indexDir)
	t.Setenv("HTTP_ADDR", "")

	settings, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.HTTPAddr != ":9090" {
		t.Fatalf("http_addr=%q want :9090 from config server.http_addr", settings.HTTPAddr)
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

func TestLoadPathContainmentStrictRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	outside := t.TempDir()

	_, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot:   dir,
		IndexDir:        outside,
		ConfigPath:      path,
		PathContainment: PathContainmentStrict,
		UseProcessEnv:   false,
	})
	if err == nil {
		t.Fatal("expected strict containment error for INDEX_DIR outside workspace")
	}
}

func TestLoadWithOptionsAllowlistPermitsExternalIndex(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	external := t.TempDir()

	settings, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot:   dir,
		IndexDir:        external,
		ConfigPath:      path,
		PathContainment: PathContainmentStrict,
		PathAllowlist:   []string{external},
		UseProcessEnv:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.IndexDir != external {
		t.Fatalf("index_dir=%q", settings.IndexDir)
	}
}

func TestLoadPathContainmentOffAllowsEscape(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	outside := t.TempDir()

	settings, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot:   dir,
		IndexDir:        outside,
		ConfigPath:      path,
		PathContainment: PathContainmentOff,
		UseProcessEnv:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.IndexDir != outside {
		t.Fatalf("index_dir=%q", settings.IndexDir)
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

func TestLoadPhase3EnvInvalidIgnored(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	t.Setenv("WORKSPACE_ROOT", dir)
	t.Setenv("CONFIG_PATH", path)
	t.Setenv("INDEX_DIR", filepath.Join(dir, "data", "index"))
	t.Setenv("SEARCH_SLOW_THRESHOLD_SECONDS", "not-a-number")
	t.Setenv("SEARCH_STATS_WINDOW", "bad")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.App.Search.SlowThresholdSeconds != DefaultSlowSearchSeconds {
		t.Fatalf("slow=%v", settings.App.Search.SlowThresholdSeconds)
	}
}

func TestLoadPhase3EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	indexDir := filepath.Join(dir, "data", "index")

	t.Setenv("WORKSPACE_ROOT", dir)
	t.Setenv("CONFIG_PATH", path)
	t.Setenv("INDEX_DIR", indexDir)
	t.Setenv("SEARCH_SLOW_THRESHOLD_SECONDS", "9.5")
	t.Setenv("SEARCH_DEGRADE_RATIO", "3.0")
	t.Setenv("SEARCH_STATS_WINDOW", "15")
	t.Setenv("FILE_WATCHER_ENABLED", "false")
	t.Setenv("FILE_WATCHER_BACKEND", "polling")
	t.Setenv("FILE_WATCHER_POLL_INTERVAL_SECONDS", "5")
	t.Setenv("MCP_LOG_LEVEL", "debug")
	t.Setenv("MCP_LOG_VERBOSE", "true")
	t.Setenv("INDEXING_STALL_SECONDS", "90")

	settings, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.App.Search.SlowThresholdSeconds != 9.5 {
		t.Fatalf("slow=%v", settings.App.Search.SlowThresholdSeconds)
	}
	if settings.App.Search.DegradeRatio != 3.0 {
		t.Fatalf("ratio=%v", settings.App.Search.DegradeRatio)
	}
	if settings.App.Search.StatsWindow != 15 {
		t.Fatalf("window=%d", settings.App.Search.StatsWindow)
	}
	if settings.App.FileWatcher.Enabled {
		t.Fatal("expected watcher disabled")
	}
	if settings.App.FileWatcher.Backend != "polling" {
		t.Fatalf("backend=%q", settings.App.FileWatcher.Backend)
	}
	if settings.App.FileWatcher.PollIntervalSeconds != 5 {
		t.Fatalf("poll=%v", settings.App.FileWatcher.PollIntervalSeconds)
	}
	if settings.App.Logging.Level != "debug" {
		t.Fatalf("level=%q", settings.App.Logging.Level)
	}
	if !settings.App.Logging.Verbose {
		t.Fatal("expected verbose logging")
	}
	if settings.App.Indexing.StallSeconds != 90 {
		t.Fatalf("stall=%v", settings.App.Indexing.StallSeconds)
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
