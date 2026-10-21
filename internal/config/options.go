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
	CollectionID     string
	IndexDir         string
	ConfigPath       string
	EnvPath          string
	AutoIndexOnStart *bool
	HTTPAddr         string
	APIToken         string
	GitHubRepo       string
	// Secrets holds KEY=VALUE pairs from workspace .env without mutating process env.
	Secrets map[string]string
	// DotEnvPaths, when non-nil, replaces the default .env candidate list
	// (config dir, workspace install dir, EnvPath).
	DotEnvPaths []string
	// UseProcessEnv merges process environment for overrides and secret lookup (default per-project mode).
	UseProcessEnv bool
	// PathContainment controls validation of INDEX_DIR/CONFIG_PATH against workspace root.
	PathContainment PathContainmentMode
	// PathAllowlist permits paths outside workspace root (daemon Docker index mounts).
	PathAllowlist []string
}

// Load reads environment variables and YAML config (per-project compatibility wrapper).
func Load() (*Settings, error) {
	workspace := ResolveWorkspaceRoot(os.Getenv("WORKSPACE_ROOT"))
	workspaceID := envOr("WORKSPACE_ID", workspace)

	if err := loadDotEnvCandidates(workspace, filepath.Join(workspace, DefaultInstallDirName, "config.yaml")); err != nil {
		return nil, err
	}

	workspace = ResolveWorkspaceRoot(envOr("WORKSPACE_ROOT", workspace))

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
	workspace := NormalizeAbsolutePath(mustAbs(opts.WorkspaceRoot))

	workspaceID := NormalizeWorkspaceID(opts.WorkspaceID, workspace)

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

	mode := resolvePathContainmentMode(opts)
	if err := validateSettingsPaths(workspace, indexDir, configPath, mode, opts.PathAllowlist); err != nil {
		return nil, err
	}

	secrets := cloneSecrets(opts.Secrets)
	if secrets == nil {
		secrets = make(map[string]string)
	}
	dotEnvPaths := opts.DotEnvPaths
	if dotEnvPaths == nil {
		dotEnvPaths = dotEnvCandidatePaths(workspace, configPath, opts.EnvPath)
	}
	if err := mergeDotEnvIntoMap(secrets, dotEnvPaths); err != nil {
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
	if err := validateProfiles(app); err != nil {
		return nil, err
	}
	applyProfileSecrets(&app, secrets, opts.UseProcessEnv)

	autoIndex := false
	if opts.AutoIndexOnStart != nil {
		autoIndex = *opts.AutoIndexOnStart
	} else if opts.UseProcessEnv {
		autoIndex = strings.EqualFold(envOr("AUTO_INDEX_ON_START", "false"), "true")
	}

	httpAddr := DefaultHTTPAddrLocal
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

	if err := validateSettingsPaths(workspace, indexDir, configPath, resolvePathContainmentMode(opts), opts.PathAllowlist); err != nil {
		return nil, err
	}

	return &Settings{
		WorkspaceRoot:    workspace,
		WorkspaceID:      workspaceID,
		CollectionID:     opts.CollectionID,
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
	if configPath != "" {
		paths = append(paths, filepath.Join(filepath.Dir(configPath), ".env"))
	}
	paths = append(paths, filepath.Join(workspace, DefaultInstallDirName, ".env"))
	if envPath != "" {
		paths = append(paths, envPath)
	}
	return paths
}

func mergeDotEnvIntoMap(secrets map[string]string, paths []string) error {
	parsed, err := ParseDotEnv(paths...)
	if err != nil {
		return err
	}
	for k, v := range parsed {
		secrets[k] = v
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
func LoadWorkspaceFromSpec(id, root, indexDir, configPath string, opts LoadOptions) (*Settings, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	auto := false
	if opts.AutoIndexOnStart != nil {
		auto = *opts.AutoIndexOnStart
	}
	opts.WorkspaceRoot = rootAbs
	opts.WorkspaceID = id
	opts.IndexDir = indexDir
	opts.ConfigPath = configPath
	opts.UseProcessEnv = false
	if opts.AutoIndexOnStart == nil {
		opts.AutoIndexOnStart = &auto
	}
	return LoadWithOptions(opts)
}

// LoadForCollection loads settings for a global-install collection: the index
// lives under the user-level collections dir, config and secrets come from the
// global install, and per-project overrides from pc are applied on top.
func LoadForCollection(pc *ProjectConfig, workspaceRoot string, opts LoadOptions) (*Settings, error) {
	if pc == nil {
		return nil, fmt.Errorf("project config is required")
	}
	if err := ValidateCollectionID(pc.CollectionID); err != nil {
		return nil, err
	}

	if opts.IndexDir == "" {
		indexDir, err := CollectionDir(pc.CollectionID)
		if err != nil {
			return nil, err
		}
		opts.IndexDir = indexDir
	}
	if opts.ConfigPath == "" {
		configPath, err := GlobalConfigPath()
		if err != nil {
			return nil, err
		}
		opts.ConfigPath = configPath
	}

	// Secrets come from the global .env and an explicit ENV_PATH only;
	// process env stays untouched (same isolation as daemon mode).
	globalEnv, err := GlobalEnvPath()
	if err != nil {
		return nil, err
	}
	opts.DotEnvPaths = []string{globalEnv, opts.EnvPath}
	opts.UseProcessEnv = false

	// Index and config live outside the workspace root by design; recognize the
	// global data dir as a legal location for path containment checks.
	userDir, err := UserDataDir()
	if err != nil {
		return nil, err
	}
	opts.PathAllowlist = append(append([]string(nil), opts.PathAllowlist...), userDir)

	opts.WorkspaceRoot = workspaceRoot
	opts.WorkspaceID = pc.CollectionID
	opts.CollectionID = pc.CollectionID

	settings, err := LoadWithOptions(opts)
	if err != nil {
		return nil, err
	}
	// A collection has no per-project install tree, so LogsDir would resolve to
	// <userdir>/logs while the daemon actually writes <userdir>/data/logs. Point it
	// at the real location so index_status.diagnostics.log_file is usable.
	if logsDir, logErr := GlobalLogsDir(); logErr == nil {
		settings.LogsDirOverride = logsDir
	}
	// Overrides are applied first, then the merged result is validated as a
	// whole so a project cannot produce settings the global config would reject.
	if err := ApplyProjectOverrides(&settings.App, pc); err != nil {
		return nil, err
	}
	if err := ValidateCollectionSettings(settings); err != nil {
		return nil, fmt.Errorf("collection %q: %w", pc.CollectionID, err)
	}
	return settings, nil
}
