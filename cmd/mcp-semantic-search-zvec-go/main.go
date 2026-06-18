package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/crash"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/gui"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lifecycle"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/logging"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
	httptransport "github.com/1CSerg/mcp-semantic-search-zvec-go/internal/transport/http"
	mcptransport "github.com/1CSerg/mcp-semantic-search-zvec-go/internal/transport/mcp"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-version" {
			fmt.Println(version.Version)
			return 0
		}
	}

	var (
		stdio              bool
		stdioProxy         bool
		httpFlag           bool
		daemonFlag         bool
		guiFlag            bool
		httpAddr           string
		configPath         string
		daemonConfig       string
		workspaceID        string
		daemonURL          string
		stopStdioWorkspace string
		stopStdioIndexDir  string
	)
	flag.BoolVar(&stdio, "stdio", false, "Run MCP server over stdin/stdout (per-project mode)")
	flag.BoolVar(&stdioProxy, "stdio-proxy", false, "Run MCP stdio proxy to shared daemon HTTP API")
	flag.BoolVar(&httpFlag, "http", false, "Run HTTP REST API server")
	flag.BoolVar(&daemonFlag, "daemon", false, "Run shared multi-workspace HTTP daemon")
	flag.BoolVar(&guiFlag, "gui", false, "Run the Windows desktop GUI")
	flag.StringVar(&httpAddr, "http-addr", "", "HTTP listen address (default 127.0.0.1:8080 per-project, :8080 daemon, or config server.http_addr)")
	flag.StringVar(&configPath, "config", "", "Override CONFIG_PATH")
	flag.StringVar(&daemonConfig, "daemon-config", "", "Path to daemon.yaml (or WORKSPACES_CONFIG env)")
	flag.StringVar(&workspaceID, "workspace-id", "", "Workspace ID for --stdio-proxy")
	flag.StringVar(&daemonURL, "daemon-url", "", "Shared daemon base URL for --stdio-proxy (default http://127.0.0.1:8080)")
	flag.StringVar(&stopStdioWorkspace, "stop-stdio-for-workspace", "", "Stop stale --stdio MCP instances for workspace and exit")
	flag.StringVar(&stopStdioIndexDir, "index-dir", "", "Index directory for lock reclaim (optional; used with --stop-stdio-for-workspace)")
	flag.Parse()

	if stopStdioWorkspace != "" {
		return runStopStdio(stopStdioWorkspace, stopStdioIndexDir)
	}

	mode, err := resolveRunMode(runMode{
		stdio:      stdio,
		stdioProxy: stdioProxy,
		httpFlag:   httpFlag,
		daemonFlag: daemonFlag,
		guiFlag:    guiFlag,
	}, runtime.GOOS, flag.NFlag())
	if err != nil {
		switch {
		case errors.Is(err, errModeConflict):
			fmt.Fprintf(os.Stderr, "%v\n", err)
		default:
			fmt.Fprintf(os.Stderr, "usage: %s [--gui|--stdio|--stdio-proxy] [--http|--daemon] [flags]\n", filepath.Base(os.Args[0]))
			fmt.Fprintf(os.Stderr, "  At least one of --gui, --stdio, --stdio-proxy, --http, or --daemon is required.\n")
		}
		return 2
	}
	stdio, stdioProxy, httpFlag, daemonFlag, guiFlag = mode.stdio, mode.stdioProxy, mode.httpFlag, mode.daemonFlag, mode.guiFlag

	if stdioProxy && strings.TrimSpace(workspaceID) == "" {
		fmt.Fprintf(os.Stderr, "--stdio-proxy requires --workspace-id\n")
		return 2
	}
	if configPath != "" {
		_ = os.Setenv("CONFIG_PATH", configPath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if stdio || stdioProxy {
		lifecycle.StartParentWatch(ctx, stop)
		if ctx.Err() != nil {
			return 0
		}
	}

	if daemonFlag {
		return runDaemon(ctx, stop, httpAddr, daemonConfig)
	}
	if stdioProxy {
		return runStdioProxy(ctx, workspaceID, daemonURL)
	}
	if guiFlag {
		return runGUI(ctx, stop)
	}
	return runPerProject(ctx, stop, stdio, httpFlag, httpAddr)
}

type runMode struct {
	stdio      bool
	stdioProxy bool
	httpFlag   bool
	daemonFlag bool
	guiFlag    bool
}

var (
	errModeConflict = errors.New("--gui cannot be combined with --stdio, --stdio-proxy, --http, or --daemon")
	errNoMode       = errors.New("no run mode selected")
)

// resolveRunMode applies no-flag defaults and mode overrides, returning the
// effective mode or a usage error. nflag is the number of flags actually set
// (flag.NFlag()); when zero, OS-specific defaults apply. This is separated from
// run for testability, since flag.BoolVar resets bound variables to their
// declared default and would otherwise clobber pre-parse assignments.
func resolveRunMode(in runMode, goos string, nflag int) (runMode, error) {
	if nflag == 0 {
		in.stdio, in.guiFlag = defaultNoArgsModes(goos)
	}
	if in.stdioProxy {
		in.stdio = true
		in.httpFlag = false
		in.daemonFlag = false
	}
	if in.daemonFlag {
		in.httpFlag = true
		in.stdio = false
	}
	if in.guiFlag && (in.stdio || in.stdioProxy || in.httpFlag || in.daemonFlag) {
		return in, errModeConflict
	}
	if !in.stdio && !in.httpFlag && !in.guiFlag {
		return in, errNoMode
	}
	return in, nil
}

func defaultNoArgsModes(goos string) (stdio, gui bool) {
	if goos == "windows" {
		return false, true
	}
	return true, false
}

func runPerProject(ctx context.Context, stop context.CancelFunc, stdio, httpFlag bool, httpAddr string) int {
	rt, err := setupPerProject(ctx, perProjectOptions{
		Stdio:            stdio,
		StartBackgrounds: true,
	})
	if err != nil {
		slog.Error("per-project setup failed", "err", err)
		return 1
	}
	defer rt.Close()

	defer func() {
		if r := recover(); r != nil {
			_ = crash.WriteWithOptions(rt.settings.LogsDir(), version.Version, r, crash.WriteOptions{
				RedactPaths:   crash.RedactPathsEnabled(),
				WorkspaceRoot: rt.settings.WorkspaceRoot,
			})
			panic(r)
		}
	}()

	slog.Info("starting", "version", version.Version, "workspace", rt.settings.WorkspaceRoot, "mode", "per-project")
	return serveTransports(ctx, stop, rt.settings, rt.svc, httpFlag, httpAddr, stdio)
}

func runGUI(ctx context.Context, stop context.CancelFunc) int {
	rt, err := setupPerProject(ctx, perProjectOptions{
		Stdio:            false,
		StartBackgrounds: false,
	})
	if err != nil {
		slog.Error("gui setup failed", "err", err)
		return 1
	}
	defer func() {
		stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if p, ok := rt.svc.(*service.Phase1); ok {
			if err := p.Shutdown(shutdownCtx); err != nil {
				slog.Warn("gui shutdown wait", "err", err)
			}
		}
		rt.Close()
	}()

	defer func() {
		if r := recover(); r != nil {
			_ = crash.WriteWithOptions(rt.settings.LogsDir(), version.Version, r, crash.WriteOptions{
				RedactPaths:   crash.RedactPathsEnabled(),
				WorkspaceRoot: rt.settings.WorkspaceRoot,
			})
			panic(r)
		}
	}()

	slog.Info("starting", "version", version.Version, "workspace", rt.settings.WorkspaceRoot, "mode", "gui")
	if err := gui.Run(ctx, rt.settings, rt.svc); err != nil {
		slog.Error("gui error", "err", err)
		return 1
	}
	return 0
}

type perProjectRuntime struct {
	settings *config.Settings
	svc      service.Service
	cleanup  []func()
}

type perProjectOptions struct {
	Stdio            bool
	StartBackgrounds bool
}

func (rt *perProjectRuntime) Close() {
	for i := len(rt.cleanup) - 1; i >= 0; i-- {
		rt.cleanup[i]()
	}
	zvec.ShutdownRuntime()
}

func setupPerProject(ctx context.Context, opts perProjectOptions) (*perProjectRuntime, error) {
	settings, err := loadSettings()
	if err != nil {
		return nil, fmt.Errorf("config load failed: %w", err)
	}

	rt := &perProjectRuntime{settings: settings}
	_, logCloser, err := logging.Setup(settings)
	if err != nil {
		slog.Warn("file logging setup failed, using stderr only", "err", err)
	} else {
		rt.cleanup = append(rt.cleanup, func() { _ = logCloser.Close() })
	}

	if opts.Stdio {
		stdioLock, err := lifecycle.PrepareStdio(settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp-semantic-search-zvec-go: %v\n", err)
			fmt.Fprintf(os.Stderr, "Hint: restart Cursor or kill extra mcp-semantic-search-zvec-go.exe processes for this workspace.\n")
			rt.Close()
			return nil, err
		}
		rt.cleanup = append(rt.cleanup, func() { _ = stdioLock.Release() })
	}

	var svc service.Service = service.NewStub(settings)
	var phase1 *service.Phase1
	if p, err := service.NewPhase1(settings); err == nil {
		phase1 = p
		svc = phase1
		phase1.SetLifecycleContext(ctx)
		rt.cleanup = append(rt.cleanup, func() { _ = phase1.Close() })
		if opts.StartBackgrounds {
			phase1.PrepareStartup()
		}
	} else {
		slog.Warn("phase1 service init failed, using stub", "err", err)
	}

	if phase1 != nil && opts.StartBackgrounds {
		phase1.StartFileWatcher(ctx)
	}

	rt.svc = svc
	return rt, nil
}

func runDaemon(ctx context.Context, stop context.CancelFunc, httpAddr, daemonConfigPath string) int {
	daemonCfg, err := daemon.LoadConfig(daemonConfigPath)
	if err != nil {
		slog.Error("daemon config load failed", "err", err)
		return 1
	}

	settings := &config.Settings{
		HTTPAddr: config.DefaultHTTPAddrDaemon,
		APIToken: os.Getenv("API_TOKEN"),
	}
	if v := os.Getenv("HTTP_ADDR"); v != "" {
		settings.HTTPAddr = v
	}
	if httpAddr != "" {
		settings.HTTPAddr = httpAddr
	}

	_, logCloser, err := logging.Setup(settings)
	if err != nil {
		slog.Warn("file logging setup failed, using stderr only", "err", err)
	} else {
		defer logCloser.Close()
	}

	defer func() {
		if r := recover(); r != nil {
			_ = crash.WriteWithOptions(crash.DaemonLogDir(), version.Version, r, crash.WriteOptions{
				RedactPaths: crash.RedactPathsEnabled(),
			})
			panic(r)
		}
	}()

	warnIfOpenHTTP(settings.HTTPAddr, settings.APIToken)

	registry := daemon.NewRegistry(daemonCfg, ctx)
	defer registry.Close()

	httpSrv := httptransport.NewDaemon(settings, registry)
	slog.Info("starting shared daemon", "version", version.Version, "workspaces", len(daemonCfg.Workspaces))

	errCh := make(chan error, 1)
	go func() {
		defer recoverToCrashReport(crash.DaemonLogDir(), "", "")
		addr := settings.HTTPAddr
		if httpAddr != "" {
			addr = httpAddr
		}
		errCh <- httpSrv.ListenAndServe(ctx, addr)
	}()

	select {
	case err := <-errCh:
		slog.Error("daemon error", "err", err)
		stop()
		return 1
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	if err := <-errCh; err != nil {
		slog.Warn("daemon http stopped", "err", err)
	}
	return 0
}

func runStdioProxy(ctx context.Context, workspaceID, daemonURL string) int {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("mcp stdio proxy panic", "panic", fmt.Sprint(r))
			_ = crash.WriteWithOptions(crash.ProxyLogDir(), version.Version, r, crash.WriteOptions{
				RedactPaths: crash.RedactPathsEnabled(),
				WorkspaceID: workspaceID,
			})
			panic(r)
		}
	}()
	if daemonURL == "" {
		daemonURL = "http://127.0.0.1:8080"
	}
	apiToken := os.Getenv("API_TOKEN")
	svc := service.NewHTTPProxy(daemonURL, workspaceID, apiToken)
	slog.Info("starting mcp stdio proxy", "version", version.Version, "workspace_id", workspaceID, "daemon_url", daemonURL)

	errCh := make(chan error, 1)
	go func() {
		defer recoverToCrashReport(crash.ProxyLogDir(), "", workspaceID)
		errCh <- mcptransport.Run(ctx, svc)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			slog.Error("mcp proxy error", "err", err)
			return 1
		}
		return 0
	case <-ctx.Done():
		if err := <-errCh; err != nil {
			slog.Error("mcp proxy error", "err", err)
			return 1
		}
		return 0
	}
}

func serveTransports(ctx context.Context, stop context.CancelFunc, settings *config.Settings, svc service.Service, httpFlag bool, httpAddr string, stdio bool) int {
	pending := 0
	if httpFlag {
		pending++
	}
	if stdio {
		pending++
	}
	if pending == 0 {
		return 0
	}

	errCh := make(chan error, pending)

	if httpFlag {
		addr := httpAddr
		if addr == "" {
			addr = settings.HTTPAddr
		}
		warnIfOpenHTTP(addr, settings.APIToken)
		httpSrv := httptransport.New(settings, svc)
		go func() {
			defer recoverToCrashReport(settings.LogsDir(), settings.WorkspaceRoot, "")
			if err := httpSrv.ListenAndServe(ctx, addr); err != nil {
				errCh <- fmt.Errorf("http: %w", err)
				return
			}
			errCh <- nil
		}()
	}

	if stdio {
		go func() {
			defer recoverToCrashReport(settings.LogsDir(), settings.WorkspaceRoot, "")
			if err := mcptransport.Run(ctx, svc); err != nil {
				errCh <- fmt.Errorf("mcp: %w", err)
				return
			}
			errCh <- nil
		}()
	}

	return awaitTransportResults(ctx, stop, pending, errCh)
}

var transportDrainTimeout = 15 * time.Second

// awaitTransportResults waits for pending transport goroutines to report on errCh.
// A nil value means clean shutdown (e.g. MCP client disconnect on stdio).
func awaitTransportResults(ctx context.Context, stop context.CancelFunc, pending int, errCh <-chan error) int {
	var firstErr error
	for completed := 0; completed < pending; {
		select {
		case err := <-errCh:
			completed++
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if completed < pending {
				stop()
			}
		case <-ctx.Done():
			slog.Info("shutdown signal received")
			stop()
			drainDeadline := time.After(transportDrainTimeout)
			for completed < pending {
				select {
				case err := <-errCh:
					completed++
					if err != nil && firstErr == nil {
						firstErr = err
					}
				case <-drainDeadline:
					slog.Warn("transport drain timeout", "pending", pending-completed)
					if firstErr == nil {
						firstErr = context.DeadlineExceeded
					}
					if firstErr != nil {
						slog.Error("server error", "err", firstErr)
						return 1
					}
					return 0
				}
			}
			if firstErr != nil {
				slog.Error("server error", "err", firstErr)
				return 1
			}
			return 0
		}
	}

	stop()
	if firstErr != nil {
		slog.Error("server error", "err", firstErr)
		return 1
	}
	slog.Info("transports stopped")
	return 0
}

// recoverToCrashReport writes last_crash.json for a panicking goroutine and
// then re-panics so the process still aborts with the original stack. Deferred
// at the top of every transport goroutine, since the parent goroutine's recover
// cannot catch panics raised on a different stack.
func recoverToCrashReport(logDir, workspaceRoot, workspaceID string) {
	if r := recover(); r != nil {
		_ = crash.WriteWithOptions(logDir, version.Version, r, crash.WriteOptions{
			RedactPaths:   crash.RedactPathsEnabled(),
			WorkspaceRoot: workspaceRoot,
			WorkspaceID:   workspaceID,
		})
		panic(r)
	}
}

func runStopStdio(workspace, indexDir string) int {
	stopped, err := lifecycle.StopStdioForWorkspace(workspace, indexDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp-semantic-search-zvec-go: stop stdio: %v\n", err)
		return 1
	}
	for _, pid := range stopped {
		fmt.Fprintf(os.Stderr, "stopped PID %d\n", pid)
	}
	return 0
}

// warnIfOpenHTTP warns when the HTTP API is exposed without an API_TOKEN,
// loudly if it binds to a non-loopback interface (reachable from the network).
func warnIfOpenHTTP(addr, token string) {
	if strings.TrimSpace(token) != "" {
		return
	}
	if isLoopbackAddr(addr) {
		slog.Warn("HTTP API has no API_TOKEN; all endpoints are unauthenticated", "addr", addr)
		return
	}
	slog.Warn("HTTP API has no API_TOKEN and binds to a non-loopback address; the API is open to the network. Set API_TOKEN or bind to 127.0.0.1", "addr", addr)
}

func isLoopbackAddr(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[:i]
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

func loadSettings() (*config.Settings, error) {
	if os.Getenv("CONFIG_PATH") == "" && os.Getenv("WORKSPACE_ROOT") == "" {
		if wd, err := os.Getwd(); err == nil {
			repoConfig := filepath.Join(wd, "config.yaml")
			if _, err := os.Stat(repoConfig); err == nil {
				_ = os.Setenv("WORKSPACE_ROOT", wd)
				_ = os.Setenv("CONFIG_PATH", repoConfig)
				_ = os.Setenv("INDEX_DIR", filepath.Join(wd, config.DefaultInstallDirName, "data", "index"))
			}
		}
	}
	return config.Load()
}
