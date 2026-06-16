package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// LoadOptions controls explicit workspace settings loading (daemon-safe).
type LoadOptions struct {
	WorkspaceRoot    string
	WorkspaceID      string
	IndexDir         string
	ConfigPath       string
	EnvPath          string
	AutoIndexOnStart *bool
	HTTPAddr         string
	APIToken         string
	GitHubRepo       string
	// Secrets holds KEY=VALUE pairs from workspace .env without mutating process env.
	Secrets map[string]string
	// UseProcessEnv merges process environment for overrides and secret lookup (default per-project mode).
	UseProcessEnv bool
}

// Load reads environment variables and YAML config (per-project compatibility wrapper).
func Load() (*Settings, error) {
	workspace := ResolveWorkspaceRoot(os.Getenv("WORKSPACE_ROOT"))
	workspaceID := envOr("WORKSPACE_ID", workspace)

	indexRaw := envOr("INDEX_DIR", filepath.Join(workspace, DefaultInstallDirName, DefaultIndexSubdir))
	indexDir, err := absPath(indexRaw, workspace)
	if err != nil {
		return nil, err
	}

	configRaw := envOr("CONFIG_PATH", filepath.Join(workspace, DefaultInstallDirName, "config.yaml"))
	configPath, err := absPath(configRaw, workspace)
	if err != nil {
		return nil, err
	}

	if err := loadDotEnvCandidates(workspace, configPath); err != nil {
		return nil, err
	}

	auto := ParseBoolEnv(envOr("AUTO_INDEX_ON_START", "false"))
	return LoadWithOptions(LoadOptions{
		WorkspaceRoot:    workspace,
		WorkspaceID:      workspaceID,
		IndexDir:         indexDir,
		ConfigPath:       configPath,
		EnvPath:          os.Getenv("ENV_PATH"),
		AutoIndexOnStart: &auto,
		UseProcessEnv:    true,
	})
}

// LoadWithOptions loads settings from explicit paths and optional local secrets map.
func LoadWithOptions(opts LoadOptions) (*Settings, error) {
	if opts.WorkspaceRoot == "" {
		opts.WorkspaceRoot = mustAbs(getwd())
	}
	workspace := mustAbs(opts.WorkspaceRoot)

	workspaceID := opts.WorkspaceID
	if workspaceID == "" {
		workspaceID = workspace
	}

	indexRaw := opts.IndexDir
	if indexRaw == "" {
		indexRaw = filepath.Join(workspace, DefaultInstallDirName, DefaultIndexSubdir)
	}
	indexDir, err := absPath(indexRaw, workspace)
	if err != nil {
		return nil, err
	}

	configRaw := opts.ConfigPath
	if configRaw == "" {
		configRaw = filepath.Join(workspace, DefaultInstallDirName, "config.yaml")
	}
	configPath, err := absPath(configRaw, workspace)
	if err != nil {
		return nil, err
	}

	secrets := cloneSecrets(opts.Secrets)
	if secrets == nil {
		secrets = make(map[string]string)
	}
	if err := mergeDotEnvIntoMap(secrets, dotEnvCandidatePaths(workspace, configPath, opts.EnvPath)); err != nil {
		return nil, err
	}

	app, err := LoadAppConfig(configPath)
	if err != nil {
		return nil, err
	}
	warnPlaintextAPIKeys(&app, configPath)
	if opts.UseProcessEnv {
		applyEnvOverrides(&app)
	}
	applyProfileSecrets(&app, secrets, opts.UseProcessEnv)

	autoIndex := false
	if opts.AutoIndexOnStart != nil {
		autoIndex = *opts.AutoIndexOnStart
	} else if opts.UseProcessEnv {
		autoIndex = strings.EqualFold(envOr("AUTO_INDEX_ON_START", "false"), "true")
	}

	httpAddr := DefaultHTTPAddr
	if app.Server.HTTPAddr != "" {
		httpAddr = app.Server.HTTPAddr
	}
	if opts.HTTPAddr != "" {
		httpAddr = opts.HTTPAddr
	} else if opts.UseProcessEnv && os.Getenv("HTTP_ADDR") != "" {
		httpAddr = os.Getenv("HTTP_ADDR")
	}

	apiToken := opts.APIToken
	if apiToken == "" && opts.UseProcessEnv {
		apiToken = os.Getenv("API_TOKEN")
	}
	if apiToken == "" {
		if v := lookupSecret(secrets, "API_TOKEN", opts.UseProcessEnv); v != "" {
			apiToken = v
		}
	}

	githubRepo := opts.GitHubRepo
	if githubRepo == "" {
		if opts.UseProcessEnv {
			githubRepo = envOr("GITHUB_REPO", "1CSerg/mcp-semantic-search-zvec-go")
		} else {
			githubRepo = "1CSerg/mcp-semantic-search-zvec-go"
		}
	}

	return &Settings{
		WorkspaceRoot:    workspace,
		WorkspaceID:      workspaceID,
		IndexDir:         indexDir,
		ConfigPath:       configPath,
		AutoIndexOnStart: autoIndex,
		GitHubRepo:       githubRepo,
		HTTPAddr:         httpAddr,
		APIToken:         apiToken,
		App:              app,
	}, nil
}

func dotEnvCandidatePaths(workspace, configPath, envPath string) []string {
	var paths []string
	if envPath != "" {
		paths = append(paths, envPath)
	}
	if configPath != "" {
		paths = append(paths, filepath.Join(filepath.Dir(configPath), ".env"))
	}
	paths = append(paths, filepath.Join(workspace, DefaultInstallDirName, ".env"))
	return paths
}

func mergeDotEnvIntoMap(secrets map[string]string, paths []string) error {
	parsed, err := ParseDotEnv(paths...)
	if err != nil {
		return err
	}
	for k, v := range parsed {
		if _, exists := secrets[k]; !exists {
			secrets[k] = v
		}
	}
	return nil
}

func cloneSecrets(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// warnPlaintextAPIKeys flags profiles that hardcode api_key in YAML, which leaks
// the secret to disk. Prefer api_key_env + .env (kept out of version control).
func warnPlaintextAPIKeys(app *AppConfig, configPath string) {
	for name, p := range app.Profiles {
		if strings.TrimSpace(p.APIKey) != "" {
			slog.Warn("plaintext api_key in config.yaml is insecure; use api_key_env with a .env secret instead",
				"profile", name, "config", configPath)
		}
	}
}

func applyProfileSecrets(app *AppConfig, secrets map[string]string, useProcessEnv bool) {
	for name, p := range app.Profiles {
		if p.APIKey != "" {
			continue
		}
		if p.APIKeyEnv != "" {
			if v := lookupSecret(secrets, p.APIKeyEnv, useProcessEnv); v != "" {
				p.APIKey = v
			}
		}
		app.Profiles[name] = p
	}
}

func lookupSecret(secrets map[string]string, key string, useProcessEnv bool) string {
	if secrets != nil {
		if v := secrets[key]; v != "" {
			return v
		}
	}
	if useProcessEnv {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}

// LoadWorkspaceFromSpec loads one daemon workspace entry.
func LoadWorkspaceFromSpec(id, root, indexDir, configPath string) (*Settings, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	auto := false
	return LoadWithOptions(LoadOptions{
		WorkspaceRoot:    rootAbs,
		WorkspaceID:      id,
		IndexDir:         indexDir,
		ConfigPath:       configPath,
		AutoIndexOnStart: &auto,
		UseProcessEnv:    false,
	})
}
