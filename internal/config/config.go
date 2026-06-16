package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultHTTPAddr          = ":8080"
	DefaultSearchLimit       = 10
	DefaultLockStaleSeconds  = 300.0
	DefaultStallSeconds      = 120.0
	DefaultHeartbeatSeconds  = 15.0
	DefaultSlowSearchSeconds = 5.0
	DefaultDegradeRatio      = 2.0
	DefaultStatsWindow       = 20
	DefaultWatcherDebounce   = 2.0
	DefaultPollInterval      = 10.0
	DefaultInstallDirName    = ".mcp-semantic-search-zvec-go"
	DefaultIndexSubdir       = "data/index"
	DefaultLogsSubdir        = "data/logs"
	DefaultEmbedMaxRetries   = 3
	DefaultEmbedRetryBaseMS  = 500
)

// Settings holds runtime configuration from environment and YAML.
type Settings struct {
	WorkspaceRoot    string
	WorkspaceID      string
	IndexDir         string
	ConfigPath       string
	AutoIndexOnStart bool
	GitHubRepo       string
	HTTPAddr         string
	APIToken         string
	App              AppConfig
}

// AppConfig is the parsed config.yaml.
type AppConfig struct {
	ActiveProfile string                      `yaml:"active_profile"`
	Profiles      map[string]EmbeddingProfile `yaml:"profiles"`
	Indexing      IndexingConfig              `yaml:"indexing"`
	Search        SearchConfig                `yaml:"search"`
	FileWatcher   FileWatcherConfig           `yaml:"file_watcher"`
	Logging       LoggingConfig               `yaml:"logging"`
	Server        ServerConfig                `yaml:"server"`
}

type EmbeddingProfile struct {
	Description    string            `yaml:"description"`
	Provider       string            `yaml:"provider"` // openai_compatible | onnx
	Model          string            `yaml:"model"`
	Dimensions     int               `yaml:"dimensions"`
	ModelPath      string            `yaml:"model_path"`
	BaseURL        string            `yaml:"base_url"`
	APIKeyEnv      string            `yaml:"api_key_env"`
	APIKey         string            `yaml:"api_key"`
	BatchSize      int               `yaml:"batch_size"`
	TimeoutSeconds float64           `yaml:"timeout_seconds"`
	MaxRetries     int               `yaml:"max_retries"`
	RetryBaseMS    int               `yaml:"retry_base_ms"`
	ExtraHeaders   map[string]string `yaml:"extra_headers"`
}

type IndexingConfig struct {
	Extensions       []string `yaml:"extensions"`
	SkipDirs         []string `yaml:"skip_dirs"`
	LockStaleSeconds float64  `yaml:"lock_stale_seconds"`
	StallSeconds     float64  `yaml:"stall_seconds"`
	HeartbeatSeconds float64  `yaml:"heartbeat_seconds"`
}

type SearchConfig struct {
	SlowThresholdSeconds float64 `yaml:"slow_threshold_seconds"`
	DegradeRatio         float64 `yaml:"degrade_ratio"`
	StatsWindow          int     `yaml:"stats_window"`
	StatsMinSamples      int     `yaml:"stats_min_samples"`
}

type FileWatcherConfig struct {
	Enabled             bool    `yaml:"enabled"`
	DebounceSeconds     float64 `yaml:"debounce_seconds"`
	RunAsDaemon         bool    `yaml:"run_as_daemon"`
	Backend             string  `yaml:"backend"` // auto | inotify | polling
	PollIntervalSeconds float64 `yaml:"poll_interval_seconds"`
}

type LoggingConfig struct {
	Level       string `yaml:"level"`
	Verbose     bool   `yaml:"verbose"`
	MaxBytes    int    `yaml:"max_bytes"`
	BackupCount int    `yaml:"backup_count"`
}

type ServerConfig struct {
	HTTPAddr string `yaml:"http_addr"`
}

// LoadAppConfig parses YAML from path.
func LoadAppConfig(path string) (AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var app AppConfig
	if err := yaml.Unmarshal(data, &app); err != nil {
		return AppConfig{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	applyAppDefaults(&app)
	return app, nil
}

func applyAppDefaults(app *AppConfig) {
	if app.Indexing.LockStaleSeconds == 0 {
		app.Indexing.LockStaleSeconds = DefaultLockStaleSeconds
	}
	if app.Indexing.StallSeconds == 0 {
		app.Indexing.StallSeconds = DefaultStallSeconds
	}
	if app.Indexing.HeartbeatSeconds == 0 {
		app.Indexing.HeartbeatSeconds = DefaultHeartbeatSeconds
	}
	if app.Search.SlowThresholdSeconds == 0 {
		app.Search.SlowThresholdSeconds = DefaultSlowSearchSeconds
	}
	if app.Search.DegradeRatio == 0 {
		app.Search.DegradeRatio = DefaultDegradeRatio
	}
	if app.Search.StatsWindow == 0 {
		app.Search.StatsWindow = DefaultStatsWindow
	}
	if app.Search.StatsMinSamples == 0 {
		app.Search.StatsMinSamples = 5
	}
	if app.Logging.Level == "" {
		app.Logging.Level = "INFO"
	}
	if app.Logging.MaxBytes == 0 {
		app.Logging.MaxBytes = 5242880
	}
	if app.Logging.BackupCount == 0 {
		app.Logging.BackupCount = 3
	}
	if app.FileWatcher.DebounceSeconds == 0 {
		app.FileWatcher.DebounceSeconds = DefaultWatcherDebounce
	}
	if app.FileWatcher.PollIntervalSeconds == 0 {
		app.FileWatcher.PollIntervalSeconds = DefaultPollInterval
	}
	if app.FileWatcher.Backend == "" {
		app.FileWatcher.Backend = "auto"
	}
	for name, p := range app.Profiles {
		if p.BatchSize == 0 {
			p.BatchSize = 32
		}
		if p.TimeoutSeconds == 0 {
			p.TimeoutSeconds = 60
		}
		if p.MaxRetries == 0 {
			p.MaxRetries = DefaultEmbedMaxRetries
		}
		if p.RetryBaseMS == 0 {
			p.RetryBaseMS = DefaultEmbedRetryBaseMS
		}
		app.Profiles[name] = p
	}
}

// ActiveProfile returns the selected embedding profile or error.
func (s *Settings) ActiveProfile() (EmbeddingProfile, error) {
	name := s.App.ActiveProfile
	if name == "" {
		return EmbeddingProfile{}, fmt.Errorf("active_profile is not set in config")
	}
	p, ok := s.App.Profiles[name]
	if !ok {
		return EmbeddingProfile{}, fmt.Errorf("profile %q not found in config", name)
	}
	return p, nil
}

// LogsDir returns the log directory under install tree.
func (s *Settings) LogsDir() string {
	return filepath.Join(filepath.Dir(filepath.Dir(s.IndexDir)), "logs")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func absPath(raw, workspace string) (string, error) {
	if filepath.IsAbs(raw) {
		return filepath.Abs(raw)
	}
	return filepath.Abs(filepath.Join(workspace, raw))
}

// ParseBoolEnv parses common truthy strings.
func ParseBoolEnv(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func applyEnvOverrides(app *AppConfig) {
	if v := os.Getenv("SEARCH_SLOW_THRESHOLD_SECONDS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			app.Search.SlowThresholdSeconds = f
		}
	}
	if v := os.Getenv("SEARCH_DEGRADE_RATIO"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			app.Search.DegradeRatio = f
		}
	}
	if v := os.Getenv("SEARCH_STATS_WINDOW"); v != "" {
		app.Search.StatsWindow = ParseIntEnv("SEARCH_STATS_WINDOW", app.Search.StatsWindow)
	}
	if v := os.Getenv("INDEXING_STALL_SECONDS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			app.Indexing.StallSeconds = f
		}
	}
	if v := os.Getenv("FILE_WATCHER_ENABLED"); v != "" {
		app.FileWatcher.Enabled = ParseBoolEnv(v)
	}
	if v := os.Getenv("FILE_WATCHER_BACKEND"); v != "" {
		app.FileWatcher.Backend = v
	}
	if v := os.Getenv("FILE_WATCHER_POLL_INTERVAL_SECONDS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			app.FileWatcher.PollIntervalSeconds = f
		}
	}
	if v := os.Getenv("MCP_LOG_LEVEL"); v != "" {
		app.Logging.Level = v
	}
	if v := os.Getenv("MCP_LOG_VERBOSE"); v != "" {
		app.Logging.Verbose = ParseBoolEnv(v)
	}
	if v := os.Getenv("MCP_LOG_MAX_BYTES"); v != "" {
		app.Logging.MaxBytes = ParseIntEnv("MCP_LOG_MAX_BYTES", app.Logging.MaxBytes)
	}
	if v := os.Getenv("MCP_LOG_BACKUP_COUNT"); v != "" {
		app.Logging.BackupCount = ParseIntEnv("MCP_LOG_BACKUP_COUNT", app.Logging.BackupCount)
	}
}

// ParseIntEnv parses int env with fallback.
func ParseIntEnv(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
