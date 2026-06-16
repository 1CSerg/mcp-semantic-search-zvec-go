package daemon

import (
	"fmt"
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
	PathContainment   string          `yaml:"path_containment"`
	PathAllowlist     []string        `yaml:"path_allowlist"`
	Workspaces        []WorkspaceSpec `yaml:"workspaces"`
}

// WorkspaceSpec registers one project workspace.
type WorkspaceSpec struct {
	ID         string `yaml:"id"`
	Root       string `yaml:"root"`
	IndexDir   string `yaml:"index_dir"`
	ConfigPath string `yaml:"config_path"`
}

// LoadConfig reads daemon.yaml from path or WORKSPACES_CONFIG env.
func LoadConfig(path string) (Config, error) {
	if path == "" {
		path = os.Getenv(envWorkspacesConfig)
	}
	if path == "" {
		return Config{}, fmt.Errorf("daemon config path is required (flag --daemon-config or %s)", envWorkspacesConfig)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read daemon config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse daemon config %s: %w", path, err)
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
	if len(cfg.Workspaces) == 0 {
		return fmt.Errorf("daemon config: workspaces list is empty")
	}
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

		if strings.TrimSpace(spec.IndexDir) == "" {
			spec.IndexDir = filepath.Join(root, ".mcp-semantic-search-zvec-go", "data", "index")
		}
		indexDir, err := absPathIfRelative(spec.IndexDir, root)
		if err != nil {
			return fmt.Errorf("workspace %q index_dir: %w", spec.ID, err)
		}
		spec.IndexDir = indexDir
		if err := config.ValidatePathContainment(config.PathContainmentOptions{
			Mode:         mode,
			FieldName:    "index_dir",
			Path:         indexDir,
			AllowedRoots: []string{root},
			Allowlist:    allowlist,
		}); err != nil {
			return fmt.Errorf("workspace %q: %w", spec.ID, err)
		}

		if strings.TrimSpace(spec.ConfigPath) == "" {
			spec.ConfigPath = filepath.Join(root, ".mcp-semantic-search-zvec-go", "config.yaml")
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
			AllowedRoots: []string{root},
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
