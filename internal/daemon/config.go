package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	defaultMaxOpenWorkspaces = 10
	envWorkspacesConfig      = "WORKSPACES_CONFIG"
)

// Config is the shared daemon registration file (daemon.yaml).
type Config struct {
	MaxOpenWorkspaces int             `yaml:"max_open_workspaces"`
	HTTPAddr          string          `yaml:"http_addr"`
	PathContainment   string          `yaml:"path_containment"`
	PathAllowlist     []string        `yaml:"path_allowlist"`
	ProjectsDirPath   string          `yaml:"projects_dir"`
	AutoIndexOnOpen   *bool           `yaml:"auto_index_on_open"`
	Workspaces        []WorkspaceSpec `yaml:"workspaces"`
}

// WorkspaceSpec registers one project workspace.
type WorkspaceSpec struct {
	ID          string `yaml:"id"`
	Root        string `yaml:"root"`
	Description string `yaml:"description"`
	IndexDir    string `yaml:"index_dir"`
	ConfigPath  string `yaml:"config_path"`
	// FromProjectsDir marks collection registrations loaded from projects.d
	// (global install layout; index under the user-level collections dir).
	FromProjectsDir bool `yaml:"-"`
	// ProjectDigest is the SHA-256 of the project.yaml this spec was built from.
	// A change here means the project's overrides changed and the open handle
	// must be reopened through a reload.
	ProjectDigest string `yaml:"-"`
}

// DefaultDaemonConfigPath returns the global daemon.yaml path under the user data dir.
func DefaultDaemonConfigPath() (string, error) {
	dir, err := config.UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.yaml"), nil
}

// LoadConfig reads daemon.yaml from path, the WORKSPACES_CONFIG env, or the
// global daemon.yaml in the user data dir when neither is set.
//
// The user-level projects.d registry is merged in only for the user-level daemon
// (no explicit config source). A legacy daemon started with --daemon-config or
// WORKSPACES_CONFIG keeps serving exactly the workspaces it was given: merging the
// global registry there would make two daemons own the same collections, and the
// legacy one does not take the singleton lock.
//
// A single damaged registration is skipped with an error log instead of failing the
// whole load: otherwise one bad file would stop the daemon from starting at all, and
// the user could not even reach the tooling that removes it. Reload is strict (see
// ConfigLoaderStrict) so applying a snapshot stays atomic.
func LoadConfig(path string) (Config, error) {
	return loadConfig(path, false)
}

// LoadConfigStrict is LoadConfig without the tolerance for damaged registrations:
// any invalid entry fails the whole load. Reload uses it so a partially valid
// snapshot is never applied.
func LoadConfigStrict(path string) (Config, error) {
	return loadConfig(path, true)
}

func loadConfig(path string, strict bool) (Config, error) {
	explicit := strings.TrimSpace(path)
	if explicit == "" {
		explicit = strings.TrimSpace(os.Getenv(envWorkspacesConfig))
	}
	if explicit == "" {
		if globalPath, err := DefaultDaemonConfigPath(); err == nil {
			if _, statErr := os.Stat(globalPath); statErr == nil {
				path = globalPath
			}
		}
	}
	var cfg Config
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read daemon config %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse daemon config %s: %w", path, err)
		}
	}

	// A legacy daemon (explicit config source) only merges projects.d when it
	// opted in by setting projects_dir itself.
	mergeProjectsDir := explicit == "" || strings.TrimSpace(cfg.ProjectsDirPath) != ""
	if mergeProjectsDir {
		projectsDir := strings.TrimSpace(cfg.ProjectsDirPath)
		if projectsDir == "" {
			pd, err := config.ProjectsDir()
			if err != nil {
				return Config{}, fmt.Errorf("resolve projects dir: %w", err)
			}
			projectsDir = pd
		}
		specs, skipped, err := LoadProjectsDirWithSkipped(projectsDir)
		if err != nil {
			return Config{}, err
		}
		if len(skipped) > 0 {
			if strict {
				first := skipped[0]
				return Config{}, fmt.Errorf("invalid project registration %s: %w", first.Path, first.Err)
			}
			for _, s := range skipped {
				slog.Error("skipping invalid project registration; fix or remove the file",
					"file", s.Path, "err", s.Err)
			}
		}
		for _, spec := range specs {
			if cfg.SpecByID(spec.ID) != nil {
				return Config{}, fmt.Errorf("daemon config: workspace id %q registered in both %s and projects.d", spec.ID, path)
			}
			cfg.Workspaces = append(cfg.Workspaces, spec)
		}
	}

	if err := normalizeConfig(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func normalizeConfig(cfg *Config) error {
	if cfg.MaxOpenWorkspaces <= 0 {
		cfg.MaxOpenWorkspaces = defaultMaxOpenWorkspaces
	}
	// An empty registry is valid: the user-level daemon starts before any project
	// is added, and projects arrive later through projects.d plus reload.
	mode := config.ParsePathContainmentMode(cfg.PathContainment)
	if strings.TrimSpace(cfg.PathContainment) == "" {
		mode = config.PathContainmentWarn
	}
	allowlist, err := config.AbsPaths(cfg.PathAllowlist)
	if err != nil {
		return fmt.Errorf("daemon config path_allowlist: %w", err)
	}
	cfg.PathAllowlist = allowlist

	seen := make(map[string]struct{}, len(cfg.Workspaces))
	seenIndexDirs := make(map[string]string, len(cfg.Workspaces))
	for i := range cfg.Workspaces {
		spec := &cfg.Workspaces[i]
		spec.ID = strings.TrimSpace(spec.ID)
		if spec.ID == "" {
			return fmt.Errorf("daemon config: workspace[%d] missing id", i)
		}
		if _, ok := seen[spec.ID]; ok {
			return fmt.Errorf("daemon config: duplicate workspace id %q", spec.ID)
		}
		seen[spec.ID] = struct{}{}

		root, err := filepath.Abs(strings.TrimSpace(spec.Root))
		if err != nil {
			return fmt.Errorf("workspace %q root: %w", spec.ID, err)
		}
		spec.Root = root

		allowedRoots := []string{root}
		if spec.FromProjectsDir {
			if userDir, uerr := config.UserDataDir(); uerr == nil {
				allowedRoots = append(allowedRoots, userDir)
			}
		}

		if strings.TrimSpace(spec.IndexDir) == "" {
			if spec.FromProjectsDir {
				collectionDir, cerr := config.CollectionDir(spec.ID)
				if cerr != nil {
					return fmt.Errorf("workspace %q index_dir: %w", spec.ID, cerr)
				}
				spec.IndexDir = collectionDir
			} else {
				spec.IndexDir = filepath.Join(root, ".mcp-semantic-search-zvec-go", "data", "index")
			}
		}
		indexDir, err := absPathIfRelative(spec.IndexDir, root)
		if err != nil {
			return fmt.Errorf("workspace %q index_dir: %w", spec.ID, err)
		}
		spec.IndexDir = indexDir
		indexDirClean := filepath.Clean(indexDir)
		if otherID, ok := seenIndexDirs[indexDirClean]; ok {
			return fmt.Errorf("daemon config: workspaces %q and %q share index_dir %q", otherID, spec.ID, indexDirClean)
		}
		seenIndexDirs[indexDirClean] = spec.ID
		if err := config.ValidatePathContainment(config.PathContainmentOptions{
			Mode:         mode,
			FieldName:    "index_dir",
			Path:         indexDir,
			AllowedRoots: allowedRoots,
			Allowlist:    allowlist,
		}); err != nil {
			return fmt.Errorf("workspace %q: %w", spec.ID, err)
		}

		if strings.TrimSpace(spec.ConfigPath) == "" {
			if spec.FromProjectsDir {
				globalConfig, cerr := config.GlobalConfigPath()
				if cerr != nil {
					return fmt.Errorf("workspace %q config_path: %w", spec.ID, cerr)
				}
				spec.ConfigPath = globalConfig
			} else {
				spec.ConfigPath = filepath.Join(root, ".mcp-semantic-search-zvec-go", "config.yaml")
			}
		}
		configPath, err := absPathIfRelative(spec.ConfigPath, root)
		if err != nil {
			return fmt.Errorf("workspace %q config_path: %w", spec.ID, err)
		}
		spec.ConfigPath = configPath
		if err := config.ValidatePathContainment(config.PathContainmentOptions{
			Mode:         mode,
			FieldName:    "config_path",
			Path:         configPath,
			AllowedRoots: allowedRoots,
			Allowlist:    allowlist,
		}); err != nil {
			return fmt.Errorf("workspace %q: %w", spec.ID, err)
		}
	}
	return nil
}

func absPathIfRelative(raw, base string) (string, error) {
	if filepath.IsAbs(raw) {
		return filepath.Abs(raw)
	}
	return filepath.Abs(filepath.Join(base, raw))
}

// SpecByID returns a workspace spec or nil.
func (c Config) SpecByID(id string) *WorkspaceSpec {
	for i := range c.Workspaces {
		if c.Workspaces[i].ID == id {
			return &c.Workspaces[i]
		}
	}
	return nil
}

// AutoIndexOnOpenEnabled reports whether workspaces index on open (default true).
func (c Config) AutoIndexOnOpenEnabled() bool {
	return c.AutoIndexOnOpen == nil || *c.AutoIndexOnOpen
}

// ConfigLoader returns a reloadable loader for the same daemon config source
// (explicit path, WORKSPACES_CONFIG env, or the global default). It is strict so a
// reload never applies a partially valid snapshot.
func ConfigLoader(path string) func() (Config, error) {
	return func() (Config, error) {
		return LoadConfigStrict(path)
	}
}

// StartupConfigLoader returns the tolerant loader used at daemon start: one damaged
// registration is skipped so the daemon can still start and serve the rest.
func StartupConfigLoader(path string) func() (Config, error) {
	return func() (Config, error) {
		return LoadConfig(path)
	}
}
