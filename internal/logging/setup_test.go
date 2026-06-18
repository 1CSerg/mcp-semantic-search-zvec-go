package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestSetup(t *testing.T) {
	dir := t.TempDir()
	settings := &config.Settings{
		IndexDir: filepath.Join(dir, config.DefaultInstallDirName, config.DefaultIndexSubdir),
		App: config.AppConfig{
			Logging: config.LoggingConfig{
				Level:       "debug",
				Verbose:     true,
				MaxBytes:    128,
				BackupCount: 2,
			},
		},
	}
	logger, closer, err := Setup(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	logger.Info("setup test")
	if _, err := os.Stat(filepath.Join(settings.LogsDir(), "server.log")); err != nil {
		t.Fatalf("server.log: %v", err)
	}
}

func TestSetupLogFileError(t *testing.T) {
	dir := t.TempDir()
	install := filepath.Join(dir, config.DefaultInstallDirName)
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	logsPath := filepath.Join(install, "logs")
	if err := os.WriteFile(logsPath, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := &config.Settings{
		IndexDir: filepath.Join(install, config.DefaultIndexSubdir),
		App: config.AppConfig{
			Logging: config.LoggingConfig{
				MaxBytes:    128,
				BackupCount: 2,
			},
		},
	}
	_, _, err := Setup(settings)
	if err == nil {
		t.Fatal("expected error when server.log path is blocked")
	}
}

func TestNopCloserClose(t *testing.T) {
	var c nopCloser
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestParseLevel(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"info", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
		{"warning", slog.LevelWarn},
	} {
		if got := parseLevel(tc.in); got != tc.want {
			t.Fatalf("parseLevel(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}
