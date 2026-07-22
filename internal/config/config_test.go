package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadAppConfigInvalidDimensions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `active_profile: test
profiles:
  test:
    provider: openai_compatible
    model: test-model
    dimensions: 0
    base_url: http://127.0.0.1:9/v1
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot: dir,
		ConfigPath:    path,
		UseProcessEnv: false,
	})
	if err == nil {
		t.Fatal("expected dimensions validation error")
	}
	if !strings.Contains(err.Error(), "dimensions must be positive") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadAppConfigYAMLColonHint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	badYAML := `active_profile: test
profiles:
  test:
    description: Foo: Bar
    provider: openai_compatible
    model: test-model
    dimensions: 384
    base_url: http://127.0.0.1:9/v1
`
	if err := os.WriteFile(path, []byte(badYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAppConfig(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "mapping values") {
		t.Fatalf("err=%q", msg)
	}
	if !strings.Contains(msg, "hint: quote scalar") {
		t.Fatalf("err=%q", msg)
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
	if settings.WorkspaceRoot != NormalizeAbsolutePath(dir) {
		t.Fatalf("workspace=%q want %q", settings.WorkspaceRoot, NormalizeAbsolutePath(dir))
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
	install := filepath.Join("/workspace", DefaultInstallDirName)
	settings := &Settings{
		ConfigPath: filepath.Join(install, "config.yaml"),
		IndexDir:   filepath.Join(install, DefaultIndexSubdir),
	}
	want := filepath.Join(install, "logs")
	if got := settings.LogsDir(); got != want {
		t.Fatalf("LogsDir=%q want %q", got, want)
	}
}

func TestLogsDirCustomIndexDir(t *testing.T) {
	install := filepath.Join("/workspace", DefaultInstallDirName)
	settings := &Settings{
		ConfigPath: filepath.Join(install, "config.yaml"),
		IndexDir:   "/var/lib/mcp/index",
	}
	want := filepath.Join(install, "logs")
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

func TestApplyAppDefaultsChunking(t *testing.T) {
	app := AppConfig{
		ActiveProfile: "onnx",
		Profiles: map[string]EmbeddingProfile{
			"onnx": {Provider: "onnx", Dimensions: 384},
		},
	}
	applyAppDefaults(&app)
	if app.Indexing.Chunking.Strategy != "hybrid" {
		t.Fatalf("strategy=%q", app.Indexing.Chunking.Strategy)
	}
	if app.Indexing.Chunking.MinChunkTokens != 10 {
		t.Fatalf("min_chunk_tokens=%d", app.Indexing.Chunking.MinChunkTokens)
	}
	if app.Indexing.Chunking.Version != 2 {
		t.Fatalf("version=%d", app.Indexing.Chunking.Version)
	}
	if got := app.Profiles["onnx"].MaxInputTokens; got != 256 {
		t.Fatalf("onnx max_input_tokens=%d want 256", got)
	}
	if got := app.Profiles["onnx"].EmbedBudgetRatio; got != 0.90 {
		t.Fatalf("onnx embed_budget_ratio=%v want 0.90", got)
	}
}

func TestChunkingEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	indexDir := filepath.Join(dir, "data", "index")

	t.Setenv("WORKSPACE_ROOT", dir)
	t.Setenv("CONFIG_PATH", path)
	t.Setenv("INDEX_DIR", indexDir)
	t.Setenv("CHUNKING_STRATEGY", "line_window")
	t.Setenv("CHUNKING_VERSION", "2")
	t.Setenv("EMBED_MAX_INPUT_TOKENS", "1024")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.App.Indexing.Chunking.Strategy != "line_window" {
		t.Fatalf("strategy=%q", settings.App.Indexing.Chunking.Strategy)
	}
	if settings.App.Indexing.Chunking.Version != 2 {
		t.Fatalf("version=%d", settings.App.Indexing.Chunking.Version)
	}
	if got := settings.App.Profiles["test"].MaxInputTokens; got != 1024 {
		t.Fatalf("max_input_tokens=%d", got)
	}
}

func TestApplyAppDefaultsOpenAIMaxInputTokens(t *testing.T) {
	app := AppConfig{
		ActiveProfile: "api",
		Profiles: map[string]EmbeddingProfile{
			"api": {Provider: "openai_compatible", Dimensions: 384},
		},
	}
	applyAppDefaults(&app)
	if got := app.Profiles["api"].MaxInputTokens; got != 512 {
		t.Fatalf("max_input_tokens=%d want 512", got)
	}
	if got := app.Profiles["api"].EmbedBudgetRatio; got != 0.50 {
		t.Fatalf("embed_budget_ratio=%v want 0.50", got)
	}
}

func TestApplyAppDefaultsOpenAIProvider(t *testing.T) {
	app := AppConfig{
		ActiveProfile: "api",
		Profiles: map[string]EmbeddingProfile{
			"api": {Provider: "openai", Dimensions: 384},
		},
	}
	applyAppDefaults(&app)
	if got := app.Profiles["api"].MaxInputTokens; got != 512 {
		t.Fatalf("max_input_tokens=%d want 512", got)
	}
}

func TestApplyAppDefaultsUnknownProviderMaxInputTokens(t *testing.T) {
	app := AppConfig{
		ActiveProfile: "custom",
		Profiles: map[string]EmbeddingProfile{
			"custom": {Provider: "custom", Dimensions: 384},
		},
	}
	applyAppDefaults(&app)
	if got := app.Profiles["custom"].MaxInputTokens; got != 512 {
		t.Fatalf("max_input_tokens=%d want 512", got)
	}
}

func TestApplyEnvOverridesIgnoresInvalidValues(t *testing.T) {
	app := AppConfig{
		ActiveProfile: "test",
		Profiles: map[string]EmbeddingProfile{
			"test": {Provider: "openai_compatible", Dimensions: 384, MaxInputTokens: 512},
		},
		Search:   SearchConfig{SlowThresholdSeconds: 5.0, DegradeRatio: 2.0, StatsWindow: 100},
		Indexing: IndexingConfig{StallSeconds: 30, MaxFileBytes: 1_000_000},
		Logging:  LoggingConfig{MaxBytes: 1_000_000, BackupCount: 3},
	}
	t.Setenv("SEARCH_SLOW_THRESHOLD_SECONDS", "not-a-float")
	t.Setenv("SEARCH_DEGRADE_RATIO", "-1")
	t.Setenv("SEARCH_STATS_WINDOW", "nope")
	t.Setenv("INDEXING_STALL_SECONDS", "bad")
	t.Setenv("INDEXING_MAX_FILE_BYTES", "0")
	t.Setenv("INDEXING_STREAM_CHUNK_THRESHOLD_BYTES", "x")
	t.Setenv("INDEXING_MAX_LINE_BYTES", "0")
	t.Setenv("FILE_WATCHER_POLL_INTERVAL_SECONDS", "-5")
	t.Setenv("EMBED_MAX_INPUT_TOKENS", "nope")
	applyEnvOverrides(&app)
	if app.Search.SlowThresholdSeconds != 5.0 || app.Search.DegradeRatio != 2.0 {
		t.Fatalf("search=%+v", app.Search)
	}
	if app.Search.StatsWindow != 100 {
		t.Fatalf("stats_window=%d", app.Search.StatsWindow)
	}
	if app.Indexing.MaxFileBytes != 1_000_000 {
		t.Fatalf("max_file_bytes=%d", app.Indexing.MaxFileBytes)
	}
	if app.Profiles["test"].MaxInputTokens != 512 {
		t.Fatalf("max_input_tokens=%d", app.Profiles["test"].MaxInputTokens)
	}
}

func TestValidateProfilesHybridRequiresMaxInputTokens(t *testing.T) {
	app := AppConfig{
		Indexing: IndexingConfig{
			Chunking: ChunkingConfig{Strategy: "hybrid"},
		},
		Profiles: map[string]EmbeddingProfile{
			"bad": {Provider: "openai_compatible", Dimensions: 384, MaxInputTokens: -1},
		},
	}
	err := validateProfiles(app)
	if err == nil {
		t.Fatal("expected hybrid max_input_tokens validation error")
	}
	if !strings.Contains(err.Error(), "max_input_tokens must be positive") {
		t.Fatalf("err=%v", err)
	}
}
