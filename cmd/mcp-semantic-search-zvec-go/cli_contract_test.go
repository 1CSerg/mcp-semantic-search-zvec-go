package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

// cliContractTimeout bounds every subprocess case. A command that hangs (an old
// build that starts the desktop GUI, for example) must fail the test instead of
// blocking the suite, so the child is killed when the deadline expires.
const cliContractTimeout = 10 * time.Second

var (
	cliStubOnce sync.Once
	cliStubBin  string
	cliStubErr  error
)

// TestMain removes the temporary stub binary built by the contract tests.
func TestMain(m *testing.M) {
	code := m.Run()
	if cliStubBin != "" {
		_ = os.RemoveAll(filepath.Dir(cliStubBin))
	}
	os.Exit(code)
}

// cliStubBinary builds the real command once with CGO disabled. The headless CLI
// contract does not need zvec/ONNX/tree-sitter, and CGO_ENABLED=0 keeps the check
// runnable on a machine without the native toolchain.
func cliStubBinary(t *testing.T) string {
	t.Helper()
	cliStubOnce.Do(func() {
		dir, err := os.MkdirTemp("", "msszgo-cli-contract-*")
		if err != nil {
			cliStubErr = err
			return
		}
		name := "mcp-semantic-search-zvec-go"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		bin := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", bin, ".")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			cliStubErr = fmt.Errorf("go build stub: %w\n%s", err, out)
			_ = os.RemoveAll(dir)
			return
		}
		cliStubBin = bin
	})
	if cliStubErr != nil {
		t.Fatalf("stub build failed: %v", cliStubErr)
	}
	return cliStubBin
}

// cliSandbox is an isolated set of absolute paths for one subprocess case. Nothing
// in it exists before the run, so any created entry is a side effect of the CLI.
type cliSandbox struct {
	root         string
	globalDir    string
	workspace    string
	indexDir     string
	configPath   string
	envPath      string
	localAppData string
	appData      string
	home         string
	workDir      string
}

func newCLISandbox(t *testing.T) *cliSandbox {
	t.Helper()
	root := t.TempDir()
	return &cliSandbox{
		root:         root,
		globalDir:    filepath.Join(root, "global"),
		workspace:    filepath.Join(root, "workspace"),
		indexDir:     filepath.Join(root, "index"),
		configPath:   filepath.Join(root, "config", "config.yaml"),
		envPath:      filepath.Join(root, "config", ".env"),
		localAppData: filepath.Join(root, "localappdata"),
		appData:      filepath.Join(root, "appdata"),
		home:         filepath.Join(root, "home"),
		workDir:      root,
	}
}

// env returns an environment in which every path the application may derive points
// inside the sandbox. Inherited values for those variables are dropped, otherwise a
// developer machine with a real global install would leak into the assertions.
func (s *cliSandbox) env() []string {
	overrides := map[string]string{
		"MCP_GLOBAL_DIR":  s.globalDir,
		"WORKSPACE_ROOT":  s.workspace,
		"INDEX_DIR":       s.indexDir,
		"CONFIG_PATH":     s.configPath,
		"ENV_PATH":        s.envPath,
		"LOCALAPPDATA":    s.localAppData,
		"APPDATA":         s.appData,
		"HOME":            s.home,
		"USERPROFILE":     s.home,
		"XDG_DATA_HOME":   filepath.Join(s.root, "xdg-data"),
		"XDG_CONFIG_HOME": filepath.Join(s.root, "xdg-config"),
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		if _, drop := overrides[strings.ToUpper(name)]; drop {
			continue
		}
		env = append(env, kv)
	}
	for name, value := range overrides {
		env = append(env, name+"="+value)
	}
	return env
}

// assertNoSideEffects fails when the run created the user-level install directory,
// workspace metadata, index data or any other sandbox path.
func (s *cliSandbox) assertNoSideEffects(t *testing.T) {
	t.Helper()
	for _, path := range []string{
		s.globalDir,
		s.workspace,
		s.indexDir,
		s.localAppData,
		s.appData,
		s.home,
		filepath.Join(s.root, "xdg-data"),
		filepath.Join(s.root, "xdg-config"),
		filepath.Join(s.workspace, ".mcp-semantic-search-zvec-go"),
		filepath.Join(s.root, ".mcp-semantic-search-zvec-go"),
	} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("side effect: %s was created", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stat %s: %v", path, err)
		}
	}
	// The only path the sandbox may contain is a config file the test wrote itself.
	if _, err := os.Stat(s.configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat %s: %v", s.configPath, err)
	}
}

// writeInvalidConfig puts a config file the loader must reject into the sandbox.
func (s *cliSandbox) writeInvalidConfig(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(s.configPath, []byte("workspace_root: [unclosed\n\tbad: : yaml\n"), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
}

type cliRun struct {
	stdout string
	stderr string
	exit   int
}

func runCLI(t *testing.T, bin string, args []string, sb *cliSandbox) cliRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), cliContractTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = sb.env()
	// The working directory is inside the sandbox so a config.yaml from the
	// repository (or any parent) cannot be picked up implicitly.
	cmd.Dir = sb.workDir
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s %v: timed out after %s; child killed", filepath.Base(bin), args, cliContractTimeout)
	}
	res := cliRun{stdout: stdout.String(), stderr: stderr.String()}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("%s %v: %v", filepath.Base(bin), args, err)
		}
		res.exit = exitErr.ExitCode()
	}
	return res
}

// TestCLIHeadlessContract pins the behaviour of the shipped binary for the
// headless entrypoint: help without side effects, exit 2 for a removed GUI flag,
// and unchanged server modes.
func TestCLIHeadlessContract(t *testing.T) {
	bin := cliStubBinary(t)

	helpModes := []string{"--stdio", "--stdio-proxy", "--http", "--daemon"}

	assertHelp := func(t *testing.T, res cliRun) {
		t.Helper()
		if res.exit != 0 {
			t.Fatalf("exit=%d, want 0 (stderr: %s)", res.exit, res.stderr)
		}
		for _, mode := range helpModes {
			if !strings.Contains(res.stdout, mode) {
				t.Errorf("stdout help does not mention %s:\n%s", mode, res.stdout)
			}
		}
		if !strings.Contains(res.stdout, "usage:") {
			t.Errorf("stdout help has no usage line:\n%s", res.stdout)
		}
		if strings.TrimSpace(res.stderr) != "" {
			t.Errorf("help must not write to stderr, got: %s", res.stderr)
		}
		if strings.Contains(res.stdout, "--gui") {
			t.Errorf("help must not advertise the removed --gui flag:\n%s", res.stdout)
		}
	}

	assertUsageError := func(t *testing.T, res cliRun) {
		t.Helper()
		if res.exit != 2 {
			t.Fatalf("exit=%d, want 2 (stdout: %s stderr: %s)", res.exit, res.stdout, res.stderr)
		}
		if strings.TrimSpace(res.stderr) == "" {
			t.Errorf("usage error must explain itself on stderr, stdout: %s", res.stdout)
		}
		// A usage error is not help: stdout stays empty so nothing can leak into a
		// caller that is reading it as data (an MCP frame stream, a script).
		if strings.TrimSpace(res.stdout) != "" {
			t.Errorf("usage error must not write to stdout, got: %s", res.stdout)
		}
		if strings.Contains(res.stdout+res.stderr, "--gui") {
			t.Errorf("output must not advertise the removed --gui flag:\nstdout: %s\nstderr: %s", res.stdout, res.stderr)
		}
	}

	t.Run("no args prints help", func(t *testing.T) {
		sb := newCLISandbox(t)
		assertHelp(t, runCLI(t, bin, nil, sb))
		sb.assertNoSideEffects(t)
	})

	t.Run("-h prints help", func(t *testing.T) {
		sb := newCLISandbox(t)
		assertHelp(t, runCLI(t, bin, []string{"-h"}, sb))
		sb.assertNoSideEffects(t)
	})

	t.Run("--help prints help", func(t *testing.T) {
		sb := newCLISandbox(t)
		assertHelp(t, runCLI(t, bin, []string{"--help"}, sb))
		sb.assertNoSideEffects(t)
	})

	t.Run("help does not depend on config validity", func(t *testing.T) {
		for _, args := range [][]string{nil, {"--help"}} {
			sb := newCLISandbox(t)
			sb.writeInvalidConfig(t)
			res := runCLI(t, bin, args, sb)
			assertHelp(t, res)
			if _, err := os.Stat(sb.configPath); err != nil {
				t.Fatalf("config file must be left untouched: %v", err)
			}
			for _, path := range []string{sb.globalDir, sb.workspace, sb.indexDir} {
				if _, err := os.Stat(path); err == nil {
					t.Errorf("side effect: %s was created", path)
				}
			}
		}
	})

	t.Run("removed --gui flag is a usage error", func(t *testing.T) {
		sb := newCLISandbox(t)
		assertUsageError(t, runCLI(t, bin, []string{"--gui"}, sb))
		sb.assertNoSideEffects(t)
	})

	t.Run("--gui with a management command is a usage error", func(t *testing.T) {
		sb := newCLISandbox(t)
		projectRoot := filepath.Join(sb.root, "project")
		if err := os.MkdirAll(projectRoot, 0o755); err != nil {
			t.Fatalf("mkdir project: %v", err)
		}
		assertUsageError(t, runCLI(t, bin, []string{"--gui", "--project-add", projectRoot}, sb))
		for _, path := range []string{
			filepath.Join(projectRoot, ".mcp-semantic-search-zvec-go"),
			filepath.Join(sb.globalDir, "projects.d"),
		} {
			if _, err := os.Stat(path); err == nil {
				t.Errorf("side effect: %s was created", path)
			}
		}
	})

	t.Run("config without a mode is a usage error", func(t *testing.T) {
		sb := newCLISandbox(t)
		missing := filepath.Join(sb.root, "missing", "config.yaml")
		assertUsageError(t, runCLI(t, bin, []string{"--config", missing}, sb))
		sb.assertNoSideEffects(t)
	})

	t.Run("--version and -version keep working", func(t *testing.T) {
		for _, flag := range []string{"--version", "-version"} {
			sb := newCLISandbox(t)
			res := runCLI(t, bin, []string{flag}, sb)
			if res.exit != 0 {
				t.Fatalf("%s: exit=%d, want 0 (stderr: %s)", flag, res.exit, res.stderr)
			}
			if got := strings.TrimSpace(res.stdout); got != version.Version {
				t.Errorf("%s: stdout=%q, want %q", flag, got, version.Version)
			}
			if strings.TrimSpace(res.stderr) != "" {
				t.Errorf("%s must not write to stderr, got: %s", flag, res.stderr)
			}
			sb.assertNoSideEffects(t)
		}
	})
}
