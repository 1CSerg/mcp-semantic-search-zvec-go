package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemonclient"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
)

// projectFlags are the CLI inputs relevant to project-aware operation. They are
// validated together so mode conflicts are reported before anything is loaded.
type projectFlags struct {
	ProjectConfig   string
	StdioProxy      bool
	WorkspaceID     string
	DaemonURL       string
	ProjectAdd      string
	ProjectRemove   string
	ProjectNew      string
	ProjectRelocate string
	ProjectsReload  bool
	EnsureDaemon    bool
	StopDaemon      bool
	Yes             bool
}

// projectProxyOptions configures the project-aware MCP proxy.
type projectProxyOptions struct {
	// ProjectConfigPath is the absolute path to the project's project.yaml.
	ProjectConfigPath string
	// BaseURL overrides daemon discovery (tests).
	BaseURL string
	// Token overrides the local auth token (tests).
	Token string
	// SkipEnsureRunning disables daemon discovery/launch (tests, explicit URL).
	SkipEnsureRunning bool
	// EnsureRunning replaces daemonclient.Client.EnsureRunning (tests).
	EnsureRunning func(context.Context) (daemonclient.Connection, error)
	// ServiceFactory is a seam proving the proxy never builds a local Phase1.
	ServiceFactory func() (any, error)
}

// validateProjectFlags rejects impossible CLI combinations early.
func validateProjectFlags(in projectFlags) error {
	if err := validateSingleManagementCommand(in); err != nil {
		return err
	}
	hasProject := strings.TrimSpace(in.ProjectConfig) != ""
	if !hasProject {
		return nil
	}
	if !in.StdioProxy {
		return errors.New("--project-config requires --stdio-proxy")
	}
	if strings.TrimSpace(in.WorkspaceID) != "" {
		return errors.New("--project-config cannot be combined with --workspace-id; the project config supplies the collection")
	}
	if strings.TrimSpace(in.DaemonURL) != "" {
		return errors.New("--project-config cannot be combined with --daemon-url; the daemon is discovered from the user-level config")
	}
	return nil
}

// validateSingleManagementCommand rejects two management flags at once. Running
// only the first and exiting 0 would make a script believe both happened.
func validateSingleManagementCommand(in projectFlags) error {
	selected := make([]string, 0, 2)
	if strings.TrimSpace(in.ProjectAdd) != "" {
		selected = append(selected, "--project-add")
	}
	if strings.TrimSpace(in.ProjectRemove) != "" {
		selected = append(selected, "--project-remove")
	}
	if strings.TrimSpace(in.ProjectNew) != "" {
		selected = append(selected, "--project-new-collection")
	}
	if strings.TrimSpace(in.ProjectRelocate) != "" {
		selected = append(selected, "--project-relocate")
	}
	if in.ProjectsReload {
		selected = append(selected, "--projects-reload")
	}
	if in.EnsureDaemon {
		selected = append(selected, "--ensure-daemon")
	}
	if in.StopDaemon {
		selected = append(selected, "--stop-daemon")
	}
	if len(selected) > 1 {
		return fmt.Errorf("only one project command can run at a time, got %s", strings.Join(selected, ", "))
	}
	return nil
}

// loadProjectBinding reads and validates the project config and verifies that the
// local registration matches it. Any problem is reported before network or index
// access, so a misconfigured project never silently searches another collection.
func loadProjectBinding(projectConfigPath string) (*config.ProjectConfig, string, error) {
	path := strings.TrimSpace(projectConfigPath)
	if path == "" {
		return nil, "", errors.New("--project-config requires an absolute path to project.yaml")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", fmt.Errorf("resolve project config %q: %w", path, err)
	}
	pc, err := config.LoadProjectConfig(abs)
	if err != nil {
		return nil, "", err
	}
	// project.yaml lives at <root>/.mcp-semantic-search-zvec-go/project.yaml.
	installDir := filepath.Dir(abs)
	root := filepath.Dir(installDir)
	if filepath.Base(installDir) != config.DefaultInstallDirName {
		return nil, "", fmt.Errorf("project config %s is not inside a %s directory", abs, config.DefaultInstallDirName)
	}

	userDir, err := config.UserDataDir()
	if err != nil {
		return nil, "", err
	}
	manager := daemon.NewProjectManager(userDir)
	reg, err := manager.FindRegistration(pc.CollectionID)
	if err != nil {
		return nil, "", err
	}
	if reg == nil {
		return nil, "", fmt.Errorf(
			"collection %q is not registered on this machine; run --project-add %q first",
			pc.CollectionID, root)
	}
	if config.NormalizeAbsolutePath(reg.Root) != config.NormalizeAbsolutePath(root) {
		return nil, "", fmt.Errorf(
			"collection %q is registered for %s but this project is %s; "+
				"run --project-relocate for a move or --project-new-collection for a copy",
			pc.CollectionID, reg.Root, root)
	}
	return pc, root, nil
}

// newProjectProxyService builds the MCP service for one project. It never opens
// zvec, never builds a Phase1 and never starts a watcher: all index work belongs
// to the shared daemon.
func newProjectProxyService(opts projectProxyOptions) (service.Service, error) {
	pc, _, err := loadProjectBinding(opts.ProjectConfigPath)
	if err != nil {
		return nil, err
	}
	if opts.ServiceFactory != nil {
		// Guard seam: the factory must stay untouched in the proxy path.
		_ = opts.ServiceFactory
	}

	conn := daemonclient.Connection{BaseURL: opts.BaseURL, Token: opts.Token}
	if !opts.SkipEnsureRunning && opts.BaseURL == "" {
		ensure := opts.EnsureRunning
		if ensure == nil {
			client := &daemonclient.Client{
				Executable: currentExecutable(),
				Start:      daemonclient.StartDaemon,
			}
			ensure = client.EnsureRunning
		}
		conn, err = ensure(context.Background())
		if err != nil {
			return nil, fmt.Errorf("shared daemon unavailable: %w", err)
		}
	}
	if conn.BaseURL == "" {
		return nil, errors.New("daemon base URL is required")
	}

	proxy := service.NewHTTPProxyWithTimeout(conn.BaseURL, pc.CollectionID, conn.Token, proxyTimeoutForProject(pc))
	return proxy, nil
}

// proxyTimeoutForProject returns the HTTP client budget for this collection. It
// must use the profile the collection actually runs with: a project that pins a
// slower profile would otherwise get the global profile's (shorter) budget and
// time out while the daemon is still working.
func proxyTimeoutForProject(pc *config.ProjectConfig) time.Duration {
	path, err := config.GlobalConfigPath()
	if err != nil {
		return 0
	}
	app, err := config.LoadAppConfig(path)
	if err != nil {
		return 0
	}
	active := app.ActiveProfile
	if pc != nil && strings.TrimSpace(pc.ActiveProfile) != "" {
		active = pc.ActiveProfile
	}
	profile, ok := app.Profiles[active]
	if !ok {
		// An unknown profile is reported by the daemon; keep the default budget.
		return 0
	}
	return config.EmbedHTTPBudget(profile)
}

// currentExecutable returns the running binary path, used to launch the daemon.
func currentExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}
