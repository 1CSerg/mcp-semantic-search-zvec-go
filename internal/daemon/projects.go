package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"gopkg.in/yaml.v3"
)

// projectRegistration is one <userdir>/projects.d/<id>.yaml file written by install.
//
// The format is exactly id/root/description: index and config locations are derived
// from the user-level install and are not a project's decision. The legacy
// index_dir/config_path fields are rejected by the strict decoder so a hand-written
// file cannot smuggle an out-of-tree index past the global layout.
type projectRegistration struct {
	ID          string `yaml:"id"`
	Root        string `yaml:"root"`
	Description string `yaml:"description"`
}

// LoadProjectsDir scans dir for *.yaml/*.yml project registrations. A missing
// directory is not an error. The registration id must be a valid collection id
// and must match the file name; index/config are derived from the global layout.
//
// A single broken registration must not take the whole daemon down: one project's
// damaged file would otherwise make every collection unserviceable (and the daemon
// unable to start). Broken entries are skipped with a loud warning and returned in
// the second slice so the caller can surface them; the registry stays consistent
// with what is actually served.
func LoadProjectsDir(dir string) ([]WorkspaceSpec, error) {
	specs, skipped, err := LoadProjectsDirWithSkipped(dir)
	if err != nil {
		return nil, err
	}
	for _, s := range skipped {
		slog.Error("skipping invalid project registration; fix or remove the file",
			"file", s.Path, "err", s.Err)
	}
	return specs, nil
}

// SkippedRegistration is a projects.d entry that could not be loaded.
type SkippedRegistration struct {
	Path string
	Err  error
}

// LoadProjectsDirWithSkipped is LoadProjectsDir with the broken entries reported
// instead of only logged.
func LoadProjectsDirWithSkipped(dir string) ([]WorkspaceSpec, []SkippedRegistration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read projects dir %s: %w", dir, err)
	}
	var specs []WorkspaceSpec
	var skipped []SkippedRegistration
	seen := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, name)
		spec, err := loadProjectRegistration(path, strings.TrimSuffix(name, filepath.Ext(name)))
		if err != nil {
			skipped = append(skipped, SkippedRegistration{Path: path, Err: err})
			continue
		}
		if other, ok := seen[spec.ID]; ok {
			// A duplicate id is a registry-level conflict: serving either one would be
			// a guess, so both are skipped and the rest still works.
			skipped = append(skipped, SkippedRegistration{
				Path: path,
				Err:  fmt.Errorf("duplicate workspace id %q (also %s)", spec.ID, other),
			})
			continue
		}
		seen[spec.ID] = path
		specs = append(specs, spec)
	}
	return specs, skipped, nil
}

func loadProjectRegistration(path, fileID string) (WorkspaceSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return WorkspaceSpec{}, fmt.Errorf("read project registration %s: %w", path, err)
	}
	var reg projectRegistration
	// Strict: unknown keys (including the legacy index_dir/config_path) are errors,
	// matching the ProjectManager's writer.
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&reg); err != nil {
		return WorkspaceSpec{}, fmt.Errorf("parse project registration %s: %w", path, err)
	}
	reg.ID = strings.TrimSpace(reg.ID)
	if reg.ID == "" {
		return WorkspaceSpec{}, fmt.Errorf("project registration %s: id is required", path)
	}
	if err := config.ValidateCollectionID(reg.ID); err != nil {
		return WorkspaceSpec{}, fmt.Errorf("project registration %s: %w", path, err)
	}
	if reg.ID != fileID {
		return WorkspaceSpec{}, fmt.Errorf("project registration %s: id %q does not match file name %q", path, reg.ID, fileID)
	}
	root := strings.TrimSpace(reg.Root)
	if root == "" {
		return WorkspaceSpec{}, fmt.Errorf("project registration %s: root is required", path)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return WorkspaceSpec{}, fmt.Errorf("project registration %s: root: %w", path, err)
	}

	// The project.yaml is the authority for per-project overrides, so a snapshot
	// without a valid one is invalid: the daemon must not silently fall back to
	// global defaults for a collection whose overrides went missing.
	projectPath := config.ProjectConfigPath(root)
	projectData, err := os.ReadFile(projectPath)
	if err != nil {
		return WorkspaceSpec{}, fmt.Errorf("project registration %s: %w", path, err)
	}
	pc, err := config.ParseProjectConfig(projectData)
	if err != nil {
		return WorkspaceSpec{}, fmt.Errorf("project config %s: %w", projectPath, err)
	}
	if !strings.EqualFold(pc.CollectionID, reg.ID) {
		return WorkspaceSpec{}, fmt.Errorf(
			"project registration %s: collection_id %q does not match %s which declares %q",
			path, reg.ID, projectPath, pc.CollectionID)
	}

	// The new registration format carries no paths: the index and the shared config
	// are always derived from the user-level install, so a project cannot choose
	// where its index lives.
	indexDir, err := config.CollectionDir(reg.ID)
	if err != nil {
		return WorkspaceSpec{}, fmt.Errorf("project registration %s: %w", path, err)
	}
	configPath, err := config.GlobalConfigPath()
	if err != nil {
		return WorkspaceSpec{}, fmt.Errorf("project registration %s: %w", path, err)
	}

	description := strings.TrimSpace(reg.Description)
	if description == "" {
		description = pc.Description
	}

	return WorkspaceSpec{
		ID:              reg.ID,
		Root:            root,
		Description:     description,
		IndexDir:        indexDir,
		ConfigPath:      configPath,
		FromProjectsDir: true,
		ProjectDigest:   projectConfigDigest(projectData),
	}, nil
}

// projectConfigDigest fingerprints a project.yaml so a reload can tell whether
// the project's overrides changed since the snapshot was taken.
func projectConfigDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// LoadCollectionSettings builds settings for a projects.d collection. The
// project.yaml supplies the overrides, the index lives under the user-level
// collections dir, and config/secrets come from the global install only.
func LoadCollectionSettings(spec WorkspaceSpec, autoIndex bool, containment config.PathContainmentMode, allowlist []string) (*config.Settings, error) {
	projectPath := config.ProjectConfigPath(spec.Root)
	pc, err := config.LoadProjectConfig(projectPath)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(pc.CollectionID, spec.ID) {
		return nil, fmt.Errorf("project config %s declares collection %q but the registration says %q",
			projectPath, pc.CollectionID, spec.ID)
	}
	return config.LoadForCollection(pc, spec.Root, config.LoadOptions{
		AutoIndexOnStart: &autoIndex,
		PathContainment:  containment,
		PathAllowlist:    allowlist,
	})
}
