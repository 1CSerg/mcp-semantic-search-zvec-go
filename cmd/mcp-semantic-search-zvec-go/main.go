package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/crash"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemonclient"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk"
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
	program := filepath.Base(os.Args[0])
	args := os.Args[1:]

	for _, arg := range args {
		if arg == "--version" || arg == "-version" {
			fmt.Println(version.Version)
			return 0
		}
	}

	// No arguments and explicit help are answered before any configuration is
	// read, any directory is created and any daemon or collection is touched.
	var (
		stdio              bool
		stdioProxy         bool
		httpFlag           bool
		daemonFlag         bool
		httpAddr           string
		configPath         string
		daemonConfig       string
		workspaceID        string
		daemonURL          string
		stopStdioWorkspace string
		stopStdioIndexDir  string
		projectConfig      string
		projectAdd         string
		projectRemove      string
		projectNew         string
		projectRelocate    string
		projectsReload     bool
		ensureDaemon       bool
		stopDaemon         bool
		assumeYes          bool
	)
	fs := flag.NewFlagSet(program, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { printUsage(fs.Output(), program) }
	usageFlags = fs
	fs.BoolVar(&stdio, "stdio", false, "Run MCP server over stdin/stdout (per-project mode)")
	fs.BoolVar(&stdioProxy, "stdio-proxy", false, "Run MCP stdio proxy to shared daemon HTTP API")
	fs.BoolVar(&httpFlag, "http", false, "Run HTTP REST API server")
	fs.BoolVar(&daemonFlag, "daemon", false, "Run shared multi-workspace HTTP daemon")
	fs.StringVar(&httpAddr, "http-addr", "", "HTTP listen address (default 127.0.0.1:8080 per-project, :8080 daemon, or config server.http_addr)")
	fs.StringVar(&configPath, "config", "", "Override CONFIG_PATH")
	fs.StringVar(&daemonConfig, "daemon-config", "", "Path to daemon.yaml (or WORKSPACES_CONFIG env)")
	fs.StringVar(&workspaceID, "workspace-id", "", "Workspace ID for --stdio-proxy (legacy manual mode)")
	fs.StringVar(&daemonURL, "daemon-url", "", "Shared daemon base URL for --stdio-proxy (legacy manual mode)")
	fs.StringVar(&stopStdioWorkspace, "stop-stdio-for-workspace", "", "Stop stale --stdio MCP instances for workspace and exit")
	fs.StringVar(&stopStdioIndexDir, "index-dir", "", "Index directory for lock reclaim (optional; used with --stop-stdio-for-workspace)")
	fs.StringVar(&projectConfig, "project-config", "", "Absolute path to a project's project.yaml for --stdio-proxy (global install mode)")
	fs.StringVar(&projectAdd, "project-add", "", "Register a project root, ensure the daemon and reload it")
	fs.StringVar(&projectRemove, "project-remove", "", "Unregister a project root (collection data is kept)")
	fs.StringVar(&projectNew, "project-new-collection", "", "Give a project a new collection id (for a copied project)")
	fs.StringVar(&projectRelocate, "project-relocate", "", "Move a registered project to a new root, keeping its collection")
	fs.BoolVar(&projectsReload, "projects-reload", false, "Ask the running daemon to re-read projects.d")
	fs.BoolVar(&ensureDaemon, "ensure-daemon", false, "Start the shared daemon if it is not running")
	fs.BoolVar(&stopDaemon, "stop-daemon", false, "Stop the shared daemon gracefully")
	fs.BoolVar(&assumeYes, "yes", false, "Confirm a destructive management command (used with --project-relocate)")

	// No arguments and explicit help are answered before any configuration is
	// read, any directory is created and any daemon or collection is touched.
	if len(args) == 0 || isHelpRequest(args) {
		printUsage(os.Stdout, program)
		return 0
	}
	if err := fs.Parse(args); err != nil {
		// The FlagSet already reported the parse problem on stderr; --help is
		// intercepted above, so any error here is a usage error.
		if errors.Is(err, flag.ErrHelp) {
			printUsage(os.Stdout, program)
			return 0
		}
		return 2
	}

	if stopStdioWorkspace != "" {
		return runStopStdio(stopStdioWorkspace, stopStdioIndexDir)
	}

	projects := projectFlags{
		ProjectConfig:   projectConfig,
		StdioProxy:      stdioProxy,
		WorkspaceID:     workspaceID,
		DaemonURL:       daemonURL,
		ProjectAdd:      projectAdd,
		ProjectRemove:   projectRemove,
		ProjectNew:      projectNew,
		ProjectRelocate: projectRelocate,
		ProjectsReload:  projectsReload,
		EnsureDaemon:    ensureDaemon,
		StopDaemon:      stopDaemon,
		Yes:             assumeYes,
	}
	if err := validateProjectFlags(projects); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	if managementCommands(projects) {
		if stdio || stdioProxy || httpFlag || daemonFlag {
			fmt.Fprintf(os.Stderr, "project management commands cannot be combined with a run mode\n")
			return 2
		}
		return runProjectCommands(context.Background(), projects)
	}

	mode, err := resolveRunMode(runMode{
		stdio:      stdio,
		stdioProxy: stdioProxy,
		httpFlag:   httpFlag,
		daemonFlag: daemonFlag,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		printUsage(os.Stderr, program)
		return 2
	}
	stdio, stdioProxy, httpFlag, daemonFlag = mode.stdio, mode.stdioProxy, mode.httpFlag, mode.daemonFlag

	if stdioProxy && strings.TrimSpace(workspaceID) == "" && strings.TrimSpace(projectConfig) == "" {
		fmt.Fprintf(os.Stderr, "--stdio-proxy requires --workspace-id or --project-config\n")
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
		return runStdioProxy(ctx, stdioProxyOptions{
			WorkspaceID:   workspaceID,
			DaemonURL:     daemonURL,
			ProjectConfig: projectConfig,
		})
	}
	return runPerProject(ctx, stop, stdio, httpFlag, httpAddr)
}

type runMode struct {
	stdio      bool
	stdioProxy bool
	httpFlag   bool
	daemonFlag bool
}

var errNoMode = errors.New("no run mode selected")

// resolveRunMode applies the mode overrides that are implied by another mode,
// returning the effective mode or errNoMode. Starting without arguments never
// reaches this function: run answers it with the help text, so a mode is always
// selected explicitly. It is separated from run for testability, since flag
// registration resets bound variables to their declared default and would
// otherwise clobber pre-parse assignments.
func resolveRunMode(in runMode) (runMode, error) {
	if in.stdioProxy {
		in.stdio = true
		in.httpFlag = false
		in.daemonFlag = false
	}
	if in.daemonFlag {
		in.httpFlag = true
		in.stdio = false
	}
	if !in.stdio && !in.httpFlag {
		return in, errNoMode
	}
	return in, nil
}

// printUsage writes the single help text used for no arguments, -h/--help and
// flag errors. It is the only description of the supported modes and management
// commands, so the CLI has one source of truth.
func printUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "usage: %s <mode> [flags]\n\n", program)
	fmt.Fprintf(w, "%s - semantic search MCP server, HTTP API and shared daemon.\n\n", program)
	fmt.Fprintf(w, "Run modes (at least one; --stdio and --http may be combined):\n")
	fmt.Fprintf(w, "  --stdio                        MCP server over stdin/stdout for one project\n")
	fmt.Fprintf(w, "  --stdio-proxy                  MCP stdio proxy to the shared daemon\n")
	fmt.Fprintf(w, "  --http                         HTTP REST API server for one project\n")
	fmt.Fprintf(w, "  --daemon                       Shared multi-workspace HTTP daemon\n")
	fmt.Fprintf(w, "\n  --daemon implies --http. --stdio-proxy requires --project-config (global\n")
	fmt.Fprintf(w, "  install layout) or --workspace-id (legacy manual mode).\n")
	fmt.Fprintf(w, "\nProject management commands (no run mode):\n")
	fmt.Fprintf(w, "  --project-add <root>           Register a project root, ensure the daemon, reload it\n")
	fmt.Fprintf(w, "  --project-remove <root>        Unregister a project root (collection data is kept)\n")
	fmt.Fprintf(w, "  --project-new-collection <root> Give a copied project its own collection id\n")
	fmt.Fprintf(w, "  --project-relocate <root> --yes Move a registered project to a new root\n")
	fmt.Fprintf(w, "  --projects-reload              Ask the running daemon to re-read projects.d\n")
	fmt.Fprintf(w, "  --ensure-daemon | --stop-daemon Start or stop the shared daemon\n")
	fmt.Fprintf(w, "  --stop-stdio-for-workspace <path> Stop stale --stdio instances for a workspace\n")
	fmt.Fprintf(w, "\nFlags:\n")
	if usageFlags != nil {
		prev := usageFlags.Output()
		usageFlags.SetOutput(w)
		usageFlags.PrintDefaults()
		usageFlags.SetOutput(prev)
	}
	fmt.Fprintf(w, "\nRunning without arguments prints this help and exits with status 0.\n")
}

// usageFlags is the FlagSet run registered the CLI flags on. printUsage prints its
// defaults so the help text and the parsed flags cannot drift apart.
var usageFlags *flag.FlagSet

// isHelpRequest reports whether the arguments ask for help. The request is
// answered before flag parsing so help never depends on flag values, configuration
// files or on whether the remaining arguments are valid.
func isHelpRequest(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-h", "--help", "-help":
			return true
		}
	}
	return false
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
	chunk.CloseResources()
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

	if opts.Stdio || opts.StartBackgrounds {
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
	// User-level mode: the single installed daemon that owns every collection.
	// Legacy explicit modes (WORKSPACES_CONFIG / --daemon-config / --http-addr)
	// keep their previous behaviour.
	userLevel := isUserLevelDaemon(daemonConfigPath, httpAddr)

	if userLevel {
		// Singleton: a second daemon must not open the registry or the collections.
		daemonLock, err := daemonclient.AcquireDaemonLock()
		if err != nil {
			if errors.Is(err, daemonclient.ErrDaemonAlreadyRunning) {
				fmt.Fprintf(os.Stderr, "mcp-semantic-search-zvec-go: daemon: %v\n", err)
				return 0
			}
			fmt.Fprintf(os.Stderr, "mcp-semantic-search-zvec-go: daemon lock: %v\n", err)
			return 1
		}
		defer func() { _ = daemonLock.Release() }()
	}

	daemonCfg, err := daemon.LoadConfig(daemonConfigPath)
	if err != nil {
		slog.Error("daemon config load failed", "err", err)
		return 1
	}

	settings := &config.Settings{
		HTTPAddr: config.DefaultHTTPAddrDaemon,
		APIToken: os.Getenv("API_TOKEN"),
	}
	if userLevel {
		// The user-level daemon is local-only and always authenticated.
		token, tokenErr := daemonclient.EnsureAuthToken()
		if tokenErr != nil {
			fmt.Fprintf(os.Stderr, "mcp-semantic-search-zvec-go: daemon auth token: %v\n", tokenErr)
			return 1
		}
		settings.APIToken = token
		baseURL, urlErr := daemonclient.DaemonBaseURL()
		if urlErr != nil {
			fmt.Fprintf(os.Stderr, "mcp-semantic-search-zvec-go: daemon listener: %v\n", urlErr)
			return 1
		}
		settings.HTTPAddr = strings.TrimPrefix(baseURL, "http://")
		// Logs belong to the user-level install, never to the detached child's
		// working directory.
		if logsDir, err := config.GlobalLogsDir(); err == nil {
			settings.LogsDirOverride = logsDir
		}
		// The daemon config is the user-level daemon.yaml. It is deliberately left as
		// an empty path: LoadConfig then resolves the user-level file itself AND merges
		// projects.d. Passing the path explicitly would mark it as a legacy config
		// source, which skips the registry — the daemon would start with the right
		// workspaces and then lose them on the first reload.
	} else {
		if v := os.Getenv("HTTP_ADDR"); v != "" {
			settings.HTTPAddr = v
		}
		if httpAddr != "" {
			settings.HTTPAddr = httpAddr
		}
	}

	_, logCloser, err := logging.Setup(settings)
	if err != nil {
		slog.Warn("file logging setup failed, using stderr only", "err", err)
	} else {
		defer func() { _ = logCloser.Close() }()
	}

	defer func() {
		if r := recover(); r != nil {
			_ = crash.WriteWithOptions(crash.DaemonLogDir(), version.Version, r, crash.WriteOptions{
				RedactPaths: crash.RedactPathsEnabled(),
			})
			panic(r)
		}
	}()

	if strings.TrimSpace(settings.APIToken) == "" && !isLoopbackAddr(settings.HTTPAddr) {
		slog.Error("daemon refuses to start without API_TOKEN on a non-loopback address; set API_TOKEN or bind to 127.0.0.1", "addr", settings.HTTPAddr)
		return 1
	}
	warnIfOpenHTTP(settings.HTTPAddr, settings.APIToken)

	registry := daemon.NewRegistry(daemonCfg, ctx)
	defer registry.Close()
	// Reload re-reads the same config source the daemon started with.
	registry.SetConfigLoader(daemon.ConfigLoader(daemonConfigPath))

	httpSrv := httptransport.NewDaemon(settings, registry)
	httpSrv.SetDaemonLifecycle(daemonStopFunc(stop))
	slog.Info("starting shared daemon", "version", version.Version, "workspaces", len(daemonCfg.Workspaces),
		"addr", settings.HTTPAddr, "user_level", userLevel)

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

// isUserLevelDaemon reports whether this process is the single installed daemon.
// An explicit daemon config or listen address means the operator chose a legacy
// manual daemon, which keeps its previous behaviour (bind address, token, paths).
func isUserLevelDaemon(daemonConfigPath, httpAddr string) bool {
	if strings.TrimSpace(daemonConfigPath) != "" || strings.TrimSpace(httpAddr) != "" {
		return false
	}
	if strings.TrimSpace(os.Getenv("WORKSPACES_CONFIG")) != "" {
		return false
	}
	if v := strings.TrimSpace(os.Getenv("MCP_DAEMON_USER_LEVEL")); v != "" {
		return v == "1" || strings.EqualFold(v, "true")
	}
	// A daemon started by the user with no explicit config is the user-level one.
	return true
}

// daemonStopFunc adapts a context cancel func to the HTTP lifecycle interface.
func daemonStopFunc(stop context.CancelFunc) httptransport.DaemonLifecycle {
	return daemonStopper{stop: stop}
}

type daemonStopper struct{ stop context.CancelFunc }

func (d daemonStopper) RequestShutdown(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.stop != nil {
		d.stop()
	}
	return nil
}

func newStdioProxyService(daemonURL, workspaceID, apiToken string) *service.HTTPProxy {
	settings, err := config.Load()
	if err == nil {
		if profile, perr := settings.ActiveProfile(); perr == nil {
			return service.NewHTTPProxyForProfile(daemonURL, workspaceID, apiToken, profile)
		}
	}
	return service.NewHTTPProxy(daemonURL, workspaceID, apiToken)
}

// stdioProxyOptions selects between the project-aware mode and the legacy
// manual mode. Exactly one of ProjectConfig or WorkspaceID is set.
type stdioProxyOptions struct {
	WorkspaceID   string
	DaemonURL     string
	ProjectConfig string
}

func runStdioProxy(ctx context.Context, opts stdioProxyOptions) int {
	workspaceID := opts.WorkspaceID
	projectMode := strings.TrimSpace(opts.ProjectConfig) != ""
	if projectMode {
		// In project mode stdout carries MCP frames only: every diagnostic,
		// including the binding summary, goes to stderr.
		pc, root, err := loadProjectBinding(opts.ProjectConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp-semantic-search-zvec-go: %v\n", err)
			return 1
		}
		workspaceID = pc.CollectionID
		slog.Info("project proxy binding", "collection_id", pc.CollectionID, "root", root,
			"project_config", opts.ProjectConfig)
	}

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

	var svc service.Service
	if projectMode {
		built, err := newProjectProxyService(projectProxyOptions{ProjectConfigPath: opts.ProjectConfig})
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp-semantic-search-zvec-go: %v\n", err)
			return 1
		}
		svc = built
	} else {
		if opts.DaemonURL == "" {
			opts.DaemonURL = "http://127.0.0.1:8080"
		}
		svc = newStdioProxyService(opts.DaemonURL, workspaceID, os.Getenv("API_TOKEN"))
	}
	slog.Info("starting mcp stdio proxy", "version", version.Version, "workspace_id", workspaceID,
		"project_mode", projectMode)

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
		drainDeadline := time.After(transportDrainTimeout)
		select {
		case err := <-errCh:
			if err != nil {
				slog.Error("mcp proxy error", "err", err)
				return 1
			}
			return 0
		case <-drainDeadline:
			slog.Warn("mcp proxy drain timeout")
			return 0
		}
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

// isLoopbackAddr reports whether addr binds only to a loopback interface.
// It uses net.SplitHostPort so IPv6 literals (e.g. "[::1]:8080") and the
// empty-host wildcard forms (":8080", "[::]:8080") are parsed correctly;
// the empty / IPv6-any host binds to ALL interfaces and returns false.
func isLoopbackAddr(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "127.0.0.1", "::1", "localhost", "":
		// Empty host (":8080") listens on every interface, NOT only loopback,
		// so it must NOT be treated as loopback for the network-exposure check.
		if host == "" {
			return false
		}
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
