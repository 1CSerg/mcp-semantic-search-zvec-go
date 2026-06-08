package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/crash"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lifecycle"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/logging"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
	httptransport "github.com/1CSerg/mcp-semantic-search-zvec-go/internal/transport/http"
	mcptransport "github.com/1CSerg/mcp-semantic-search-zvec-go/internal/transport/mcp"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		stdio        bool
		stdioProxy   bool
		httpFlag     bool
		daemonFlag   bool
		httpAddr     string
		configPath   string
		daemonConfig string
		workspaceID  string
		daemonURL    string
	)
	if len(os.Args) == 1 {
		stdio = true
	}
	flag.BoolVar(&stdio, "stdio", false, "Run MCP server over stdin/stdout (per-project mode)")
	flag.BoolVar(&stdioProxy, "stdio-proxy", false, "Run MCP stdio proxy to shared daemon HTTP API")
	flag.BoolVar(&httpFlag, "http", false, "Run HTTP REST API server")
	flag.BoolVar(&daemonFlag, "daemon", false, "Run shared multi-workspace HTTP daemon")
	flag.StringVar(&httpAddr, "http-addr", "", "HTTP listen address (default :8080 or config server.http_addr)")
	flag.StringVar(&configPath, "config", "", "Override CONFIG_PATH")
	flag.StringVar(&daemonConfig, "daemon-config", "", "Path to daemon.yaml (or WORKSPACES_CONFIG env)")
	flag.StringVar(&workspaceID, "workspace-id", "", "Workspace ID for --stdio-proxy")
	flag.StringVar(&daemonURL, "daemon-url", "", "Shared daemon base URL for --stdio-proxy (default http://127.0.0.1:8080)")
	flag.Parse()

	if stdioProxy {
		stdio = true
		httpFlag = false
		daemonFlag = false
	}
	if daemonFlag {
		httpFlag = true
		stdio = false
	}

	if !stdio && !httpFlag {
		fmt.Fprintf(os.Stderr, "usage: %s [--stdio|--stdio-proxy] [--http|--daemon] [flags]\n", filepath.Base(os.Args[0]))
		fmt.Fprintf(os.Stderr, "  At least one of --stdio, --stdio-proxy, --http, or --daemon is required.\n")
		return 2
	}
	if stdioProxy && strings.TrimSpace(workspaceID) == "" {
		fmt.Fprintf(os.Stderr, "--stdio-proxy requires --workspace-id\n")
		return 2
	}
	if configPath != "" {
		_ = os.Setenv("CONFIG_PATH", configPath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if daemonFlag {
		return runDaemon(ctx, stop, httpAddr, daemonConfig)
	}
	if stdioProxy {
		return runStdioProxy(ctx, workspaceID, daemonURL)
	}
	return runPerProject(ctx, stop, stdio, httpFlag, httpAddr)
}

func runPerProject(ctx context.Context, stop context.CancelFunc, stdio, httpFlag bool, httpAddr string) int {
	settings, err := loadSettings()
	if err != nil {
		slog.Error("config load failed", "err", err)
		return 1
	}

	_, logCloser, err := logging.Setup(settings)
	if err != nil {
		slog.Warn("file logging setup failed, using stderr only", "err", err)
	} else {
		defer logCloser.Close()
	}

	defer func() {
		if r := recover(); r != nil {
			_ = crash.Write(settings.LogsDir(), version.Version, settings.WorkspaceRoot, r)
			panic(r)
		}
	}()

	if stdio {
		if err := lifecycle.PrepareStdio(settings); err != nil {
			slog.Warn("stdio prepare failed", "err", err)
		}
	}

	var svc service.Service = service.NewStub(settings)
	var phase1 *service.Phase1
	if p, err := service.NewPhase1(settings); err == nil {
		phase1 = p
		svc = phase1
		if settings.AutoIndexOnStart {
			phase1.StartAutoIndex()
		}
	} else {
		slog.Warn("phase1 service init failed, using stub", "err", err)
	}

	if phase1 != nil {
		phase1.StartFileWatcher(ctx)
	}

	slog.Info("starting", "version", version.Version, "workspace", settings.WorkspaceRoot, "mode", "per-project")
	return serveTransports(ctx, stop, settings, svc, httpFlag, httpAddr, stdio)
}

func runDaemon(ctx context.Context, stop context.CancelFunc, httpAddr, daemonConfigPath string) int {
	daemonCfg, err := daemon.LoadConfig(daemonConfigPath)
	if err != nil {
		slog.Error("daemon config load failed", "err", err)
		return 1
	}

	settings := &config.Settings{
		HTTPAddr: config.DefaultHTTPAddr,
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

	registry := daemon.NewRegistry(daemonCfg, ctx)
	defer registry.Close()

	httpSrv := httptransport.NewDaemon(settings, registry)
	slog.Info("starting shared daemon", "version", version.Version, "workspaces", len(daemonCfg.Workspaces))

	errCh := make(chan error, 1)
	go func() {
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
		return 0
	}
}

func runStdioProxy(ctx context.Context, workspaceID, daemonURL string) int {
	if daemonURL == "" {
		daemonURL = "http://127.0.0.1:8080"
	}
	apiToken := os.Getenv("API_TOKEN")
	svc := service.NewHTTPProxy(daemonURL, workspaceID, apiToken)
	slog.Info("starting mcp stdio proxy", "version", version.Version, "workspace_id", workspaceID, "daemon_url", daemonURL)

	errCh := make(chan error, 1)
	go func() {
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
		return 0
	}
}

func serveTransports(ctx context.Context, stop context.CancelFunc, settings *config.Settings, svc service.Service, httpFlag bool, httpAddr string, stdio bool) int {
	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	if httpFlag {
		addr := httpAddr
		if addr == "" {
			addr = settings.HTTPAddr
		}
		httpSrv := httptransport.New(settings, svc)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := httpSrv.ListenAndServe(ctx, addr); err != nil {
				errCh <- fmt.Errorf("http: %w", err)
			}
		}()
	}

	if stdio {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mcptransport.Run(ctx, svc); err != nil {
				errCh <- fmt.Errorf("mcp: %w", err)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case err := <-errCh:
		slog.Error("server error", "err", err)
		stop()
		<-done
		return 1
	case <-ctx.Done():
		slog.Info("shutdown signal received")
		<-done
		return 0
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
