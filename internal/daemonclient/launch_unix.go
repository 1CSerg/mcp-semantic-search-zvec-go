//go:build !windows

package daemonclient

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// detachedProcAttr detaches the daemon into its own process session on Unix, so
// it is not killed when the IDE's process group receives a signal.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// startDetached launches the daemon detached from the proxy's session and
// stdio. The daemon writes to its own log files, so no inherited pipe keeps the
// IDE waiting or dies with it.
func startDetached(_ context.Context, opts LaunchOptions) error {
	if opts.Executable == "" {
		return fmt.Errorf("daemon executable path is required")
	}
	if _, err := os.Stat(opts.Executable); err != nil {
		return fmt.Errorf("daemon executable %s: %w", opts.Executable, err)
	}
	logDir := opts.LogDir
	if logDir == "" {
		return fmt.Errorf("daemon log directory is required for a detached launch")
	}
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("create log dir %s: %w", logDir, err)
	}
	stdout, err := os.OpenFile(filepath.Join(logDir, "daemon.stdout.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon stdout log: %w", err)
	}
	defer func() { _ = stdout.Close() }()
	stderr, err := os.OpenFile(filepath.Join(logDir, "daemon.stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon stderr log: %w", err)
	}
	defer func() { _ = stderr.Close() }()
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	defer func() { _ = devNull.Close() }()

	// The token travels in the environment, never on the command line.
	cmd := exec.Command(opts.Executable, "--daemon")
	cmd.Env = append(os.Environ(),
		"MCP_GLOBAL_DIR="+opts.UserDir,
		"API_TOKEN="+opts.Token,
		"MCP_DAEMON_USER_LEVEL=1",
	)
	if opts.ConfigPath != "" {
		cmd.Env = append(cmd.Env, "CONFIG_PATH="+opts.ConfigPath)
	}
	cmd.Stdin = devNull
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = detachedProcAttr()
	// Deliberately not exec.CommandContext: the daemon must outlive the proxy.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon process: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release daemon process: %w", err)
	}
	return nil
}

// StartDaemon is the production daemon starter used by Client.Start.
func StartDaemon(ctx context.Context, opts LaunchOptions) error {
	return startDetached(ctx, opts)
}
