package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

// Setup configures slog with stderr and optional rotating server.log.
func Setup(settings *config.Settings) (*slog.Logger, io.Closer, error) {
	level := parseLevel(settings.App.Logging.Level)
	opts := &slog.HandlerOptions{Level: level}
	if settings.App.Logging.Verbose {
		opts.AddSource = true
	}

	writers := []io.Writer{os.Stderr}
	var closer io.Closer = nopCloser{}
	logPath := filepath.Join(settings.LogsDir(), "server.log")
	rw, err := newRotatingWriter(logPath, settings.App.Logging.MaxBytes, settings.App.Logging.BackupCount)
	if err != nil {
		return nil, nil, fmt.Errorf("open rotating log %s: %w", logPath, err)
	}
	writers = append(writers, rw)
	closer = rw

	handler := slog.NewTextHandler(io.MultiWriter(writers...), opts)
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger, closer, nil
}

func parseLevel(raw string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
