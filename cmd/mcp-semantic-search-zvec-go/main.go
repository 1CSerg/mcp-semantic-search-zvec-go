package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lifecycle"
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
		stdio      bool
		httpFlag   bool
		httpAddr   string
		configPath string
	)
	if len(os.Args) == 1 {
		stdio = true
	}
	flag.BoolVar(&stdio, "stdio", false, "Run MCP server over stdin/stdout")
	flag.BoolVar(&httpFlag, "http", false, "Run HTTP REST API server")
	flag.StringVar(&httpAddr, "http-addr", "", "HTTP listen address (default :8080 or config server.http_addr)")
	flag.StringVar(&configPath, "config", "", "Override CONFIG_PATH")
	flag.Parse()

	if !stdio && !httpFlag {
		fmt.Fprintf(os.Stderr, "usage: %s [--stdio] [--http] [--http-addr :8080]\n", filepath.Base(os.Args[0]))
		fmt.Fprintf(os.Stderr, "  At least one of --stdio or --http is required (default: --stdio when no flags).\n")
		return 2
	}
	if configPath != "" {
		_ = os.Setenv("CONFIG_PATH", configPath)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	settings, err := loadSettings()
	if err != nil {
		slog.Error("config load failed", "err", err)
		return 1
	}

	if stdio {
		if err := lifecycle.PrepareStdio(settings); err != nil {
			slog.Warn("stdio prepare failed", "err", err)
		}
	}

	var svc service.Service = service.NewStub(settings)
	if phase1, err := service.NewPhase1(settings); err == nil {
		svc = phase1
		if settings.AutoIndexOnStart {
			phase1.StartAutoIndex()
		}
	} else {
		slog.Warn("phase1 service init failed, using stub", "err", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("starting", "version", version.Version, "workspace", settings.WorkspaceRoot)

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
	// Dev bootstrap: use repo config.yaml when install tree is not present.
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
