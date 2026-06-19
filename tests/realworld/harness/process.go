//go:build realworld && zvec

package harness

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// KillProcess9 force-kills a subprocess and waits briefly for exit.
func KillProcess9(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
	} else {
		_ = cmd.Process.Signal(syscall.SIGKILL)
	}
	waitProcessExit(t, cmd, 5*time.Second)
}

// KillProcessGraceful sends SIGTERM (Unix) or Kill (Windows) and waits for exit.
func KillProcessGraceful(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
	} else {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	waitProcessExit(t, cmd, 15*time.Second)
}

func waitProcessExit(t *testing.T, cmd *exec.Cmd, timeout time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
	}
}

// IsPortListening reports whether something accepts TCP connections on host:port.
func IsPortListening(host string, port int) bool {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// WaitPortClosed polls until the port is no longer listening or timeout.
func WaitPortClosed(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsPortListening("127.0.0.1", port) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("port %d still listening after %v", port, timeout)
}

// StartHTTPServerNoCleanup starts HTTP without registering t.Cleanup kill (for concurrency tests).
func StartHTTPServerNoCleanup(t *testing.T, repo string, port int, extraEnv ...string) *ServerProcess {
	t.Helper()
	if port == 0 {
		port = defaultHTTPPort + 200
	}
	bin := BinPath(repo)
	binDir := BinDir(repo)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cmd := exec.Command(bin, "--http", "--http-addr", addr)
	cmd.Env = prependBinToPath(BaseEnv(repo), binDir)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Dir = binDir
	if err := cmd.Start(); err != nil {
		t.Fatalf("start http server: %v", err)
	}
	base := "http://" + addr
	proc := &ServerProcess{Cmd: cmd, HTTPBase: base, Port: port}
	waitHTTPHealth(t, base)
	return proc
}

// StartMCPServerNoCleanup spawns --stdio without automatic cleanup.
func StartMCPServerNoCleanup(t *testing.T, repo string, extraEnv ...string) *exec.Cmd {
	t.Helper()
	bin := BinPath(repo)
	binDir := BinDir(repo)
	cmd := exec.Command(bin, "--stdio")
	cmd.Env = prependBinToPath(BaseEnv(repo), binDir)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Dir = binDir
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mcp stdio: %v", err)
	}
	return cmd
}

// RunCLI runs the harness binary with args and returns combined output.
func RunCLI(t *testing.T, repo string, env []string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	bin := BinPath(repo)
	binDir := BinDir(repo)
	if len(env) == 0 {
		env = BaseEnv(repo)
	}
	env = prependBinToPath(env, binDir)
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Dir = binDir
	var outBuf, errBuf bytesCapture
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			t.Fatalf("run cli %v: %v", args, err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

type bytesCapture struct {
	data []byte
}

func (b *bytesCapture) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *bytesCapture) String() string {
	return string(b.data)
}

// ProcessAlive reports whether pid is still running.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		// FindProcess always succeeds on Windows; use exit code probe via tasklist alternative.
		// Signal 0 is not supported; rely on Wait already done elsewhere.
		return true
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
