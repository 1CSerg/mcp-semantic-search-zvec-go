package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestLoadProjectsDirMissing(t *testing.T) {
	specs, err := LoadProjectsDir(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("missing projects dir must not be an error: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("specs=%v", specs)
	}
}

func TestLoadProjectsDirDefaultsToGlobalLayout(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("MCP_GLOBAL_DIR", userDir)
	dir := filepath.Join(userDir, "projects.d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeProjectYAML(t, root, "app-one", "One")
	body := "id: app-one\nroot: " + root + "\ndescription: One\n"
	if err := os.WriteFile(filepath.Join(dir, "app-one.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	specs, err := LoadProjectsDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("specs=%v", specs)
	}
	spec := specs[0]
	if spec.ID != "app-one" || spec.Description != "One" {
		t.Fatalf("spec=%+v", spec)
	}
	if !spec.FromProjectsDir {
		t.Fatal("registration must be marked FromProjectsDir")
	}
	if spec.ProjectDigest == "" {
		t.Fatal("spec must carry the project.yaml digest")
	}
	wantIndex, err := config.CollectionDir("app-one")
	if err != nil {
		t.Fatal(err)
	}
	if spec.IndexDir != wantIndex {
		t.Fatalf("index_dir=%q want %q", spec.IndexDir, wantIndex)
	}
	wantConfig, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if spec.ConfigPath != wantConfig {
		t.Fatalf("config_path=%q want %q", spec.ConfigPath, wantConfig)
	}
}

// TestLoadProjectsDirRequiresProjectConfig: a registration whose root lost its
// project.yaml is an invalid snapshot, not a silent fall back to global defaults.
func TestLoadProjectsDirRequiresProjectConfig(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	body := "id: app-one\nroot: " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "app-one.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProjectsDirStrict(dir); err == nil {
		t.Fatal("missing project.yaml must be rejected")
	}
}

// TestLoadProjectsDirRejectsIDMismatch: the registration id and the project.yaml
// collection_id must agree, otherwise a stale copy could claim another identity.
func TestLoadProjectsDirRejectsIDMismatch(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	writeProjectYAML(t, root, "other-id", "")
	body := "id: app-one\nroot: " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "app-one.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProjectsDirStrict(dir); err == nil {
		t.Fatal("id that disagrees with project.yaml must be rejected")
	}
}

// TestLoadCollectionSettingsAppliesOverrides proves a projects.d collection is
// built through LoadForCollection, so project overrides and the global profile
// are both applied before NewPhase1.
func TestLoadCollectionSettingsAppliesOverrides(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("MCP_GLOBAL_DIR", userDir)
	if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), []byte(testGlobalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeProjectYAML(t, root, "col-override", "")
	// Add an override on top of the generated config.
	cfgPath := config.ProjectConfigPath(root)
	body := "schema_version: 1\ncollection_id: col-override\nindexing:\n  extensions: [\".bsl\"]\n  chunking:\n    min_chunk_tokens: 48\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	spec, err := loadProjectRegistration(
		writeRegistrationFile(t, userDir, "col-override", root), "col-override")
	if err != nil {
		t.Fatal(err)
	}
	settings, err := LoadCollectionSettings(spec, true, config.PathContainmentWarn, nil)
	if err != nil {
		t.Fatal(err)
	}
	if settings.CollectionID != "col-override" || settings.WorkspaceID != "col-override" {
		t.Fatalf("settings ids: collection=%q workspace=%q", settings.CollectionID, settings.WorkspaceID)
	}
	if settings.WorkspaceRoot != config.NormalizeAbsolutePath(root) {
		t.Fatalf("workspace_root=%q", settings.WorkspaceRoot)
	}
	if got := settings.App.Indexing.Extensions; len(got) != 1 || got[0] != ".bsl" {
		t.Fatalf("project overrides not applied: %v", got)
	}
	if settings.App.Indexing.Chunking.MinChunkTokens != 48 {
		t.Fatalf("min_chunk_tokens=%d", settings.App.Indexing.Chunking.MinChunkTokens)
	}
	if !settings.AutoIndexOnStart {
		t.Fatal("auto_index_on_open must reach the collection settings")
	}
	wantIndex, err := config.CollectionDir("col-override")
	if err != nil {
		t.Fatal(err)
	}
	if settings.IndexDir != wantIndex {
		t.Fatalf("index_dir=%q want %q", settings.IndexDir, wantIndex)
	}

	// The same call with auto-index disabled must not enable it.
	settings, err = LoadCollectionSettings(spec, false, config.PathContainmentWarn, nil)
	if err != nil {
		t.Fatal(err)
	}
	if settings.AutoIndexOnStart {
		t.Fatal("auto index must stay disabled when the daemon config disables it")
	}
}

// loadProjectsDirStrict fails on any invalid registration, which is the contract
// reload needs (a partially valid snapshot must never be applied).
func loadProjectsDirStrict(dir string) ([]WorkspaceSpec, error) {
	specs, skipped, err := LoadProjectsDirWithSkipped(dir)
	if err != nil {
		return nil, err
	}
	if len(skipped) > 0 {
		return nil, fmt.Errorf("invalid project registration %s: %w", skipped[0].Path, skipped[0].Err)
	}
	return specs, nil
}

func writeProjectYAML(t *testing.T, root, id, description string) {
	t.Helper()
	path := config.ProjectConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "schema_version: 1\ncollection_id: " + id + "\n"
	if description != "" {
		body += "description: \"" + description + "\"\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRegistrationFile(t *testing.T, userDir, id, root string) string {
	t.Helper()
	dir := filepath.Join(userDir, "projects.d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".yaml")
	body := "id: " + id + "\nroot: " + root + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadProjectsDirRejectsIDFileNameMismatch(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	body := "id: other-id\nroot: " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "app-one.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProjectsDirStrict(dir); err == nil {
		t.Fatal("id that does not match the file name must be rejected")
	}
}

func TestLoadProjectsDirRejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	writeProjectYAML(t, root, "dup", "")
	for _, name := range []string{"dup.yaml", "dup.yml"} {
		body := "id: dup\nroot: " + root + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadProjectsDirStrict(dir); err == nil {
		t.Fatal("duplicate ids must be rejected")
	}
}

func TestLoadProjectsDirRejectsMalformedAndMissingRoot(t *testing.T) {
	cases := map[string]struct{ fileID, body string }{
		"malformed":    {"broken", "id: [unclosed\n"},
		"empty":        {"broken", ""},
		"missing root": {"no-root", "id: no-root\n"},
		"unsafe id":    {"CON", "id: CON\nroot: /tmp\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.fileID+".yaml"), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadProjectsDirStrict(dir); err == nil {
				t.Fatalf("%s registration must be rejected", name)
			}
		})
	}
}

// TestLoadConfigTolerantAtStartupStrictOnReload pins the two contracts: a damaged
// registration must not stop the daemon from starting (the user could not reach the
// tooling that removes it), but reload must reject the whole snapshot so a
// partially valid registry is never applied.
func TestLoadConfigTolerantAtStartupStrictOnReload(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("MCP_GLOBAL_DIR", userDir)
	projectsDir := filepath.Join(userDir, "projects.d")
	if err := os.MkdirAll(projectsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// One good registration and one damaged one.
	goodRoot := t.TempDir()
	writeProjectYAML(t, goodRoot, "col-good", "")
	if err := os.WriteFile(filepath.Join(projectsDir, "col-good.yaml"),
		[]byte("id: col-good\nroot: "+goodRoot+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectsDir, "broken.yaml"),
		[]byte("id: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(userDir, "daemon.yaml")
	// projects_dir is set explicitly: an explicit config path otherwise means the
	// legacy daemon, which does not merge the global registry.
	body := "projects_dir: " + projectsDir + "\nmax_open_workspaces: 5\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	// Startup: tolerant, the good collection is served.
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("startup must tolerate one damaged registration: %v", err)
	}
	if len(cfg.Workspaces) != 1 || cfg.Workspaces[0].ID != "col-good" {
		t.Fatalf("workspaces=%v", cfg.Workspaces)
	}

	// Reload: strict, nothing is applied.
	if _, err := LoadConfigStrict(cfgPath); err == nil {
		t.Fatal("reload must reject a snapshot with a damaged registration")
	}
	if _, err := ConfigLoader(cfgPath)(); err == nil {
		t.Fatal("ConfigLoader (used by reload) must be strict")
	}
}

// TestLoadProjectsDirRejectsLegacyPathFields: the new registration format carries
// no paths. A hand-written index_dir/config_path must be rejected, not silently
// honoured, so a project cannot choose an out-of-tree index.
func TestLoadProjectsDirRejectsLegacyPathFields(t *testing.T) {
	for _, field := range []string{"index_dir: /tmp/elsewhere", "config_path: /tmp/config.yaml"} {
		t.Run(field, func(t *testing.T) {
			dir := t.TempDir()
			root := t.TempDir()
			writeProjectYAML(t, root, "col-a", "")
			body := "id: col-a\nroot: " + root + "\n" + field + "\n"
			if err := os.WriteFile(filepath.Join(dir, "col-a.yaml"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadProjectsDirStrict(dir); err == nil {
				t.Fatalf("registration with %q must be rejected", field)
			}
		})
	}
}

func TestLoadProjectsDirIgnoresNonYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub.yaml"), 0o700); err != nil {
		t.Fatal(err)
	}
	specs, err := LoadProjectsDir(dir)
	if err != nil {
		t.Fatalf("non-YAML entries must be ignored: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("specs=%v", specs)
	}
}

// TestLoadConfigEmptyGlobalRegistry pins the new default: a daemon with zero
// registered projects is valid, and an unknown id never falls back to another
// collection.
func TestLoadConfigEmptyGlobalRegistry(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("MCP_GLOBAL_DIR", userDir)
	cfgPath := filepath.Join(userDir, "daemon.yaml")
	if err := os.WriteFile(cfgPath, []byte("max_open_workspaces: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("an empty global registry must be valid: %v", err)
	}
	if len(cfg.Workspaces) != 0 {
		t.Fatalf("workspaces=%v", cfg.Workspaces)
	}
	if cfg.MaxOpenWorkspaces != 3 {
		t.Fatalf("max_open_workspaces=%d", cfg.MaxOpenWorkspaces)
	}
	if !cfg.AutoIndexOnOpenEnabled() {
		t.Fatal("auto_index_on_open must default to true")
	}
}

// TestLoadConfigLegacyDoesNotMergeGlobalProjectsDir pins that a legacy daemon
// started with an explicit config source serves only the workspaces it was given.
// Merging the user-level registry there would make two daemons own the same
// collections, and the legacy one does not take the singleton lock.
func TestLoadConfigLegacyDoesNotMergeGlobalProjectsDir(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("MCP_GLOBAL_DIR", userDir)
	projectsDir := filepath.Join(userDir, "projects.d")
	if err := os.MkdirAll(projectsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeProjectYAML(t, rootB, "col-b", "")
	if err := os.WriteFile(filepath.Join(projectsDir, "col-b.yaml"),
		[]byte("id: col-b\nroot: "+rootB+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// An explicit --daemon-config means legacy mode: projects.d is not merged.
	cfgPath := filepath.Join(userDir, "legacy-daemon.yaml")
	if err := os.WriteFile(cfgPath, []byte("workspaces:\n  - id: legacy\n    root: "+rootA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 1 || cfg.Workspaces[0].ID != "legacy" {
		t.Fatalf("a legacy daemon must not merge projects.d: %v", cfg.Workspaces)
	}

	// The same legacy daemon opts in by setting projects_dir itself.
	optIn := "projects_dir: " + projectsDir + "\nworkspaces:\n  - id: legacy\n    root: " + rootA + "\n"
	if err := os.WriteFile(cfgPath, []byte(optIn), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 2 {
		t.Fatalf("an explicit projects_dir must merge: %v", cfg.Workspaces)
	}

	// The user-level daemon (no explicit source) does merge the global registry.
	if err := os.Remove(cfgPath); err != nil {
		t.Fatal(err)
	}
	globalCfg := filepath.Join(userDir, "daemon.yaml")
	if err := os.WriteFile(globalCfg, []byte("max_open_workspaces: 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 1 || cfg.Workspaces[0].ID != "col-b" {
		t.Fatalf("the user-level daemon must merge projects.d: %v", cfg.Workspaces)
	}
}

// TestLoadConfigProjectsDirDuplicateAcrossSources checks that the same id in both
// daemon.yaml and projects.d is an error rather than a silent shadow.
func TestLoadConfigProjectsDirDuplicateAcrossSources(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("MCP_GLOBAL_DIR", userDir)
	projectsDir := filepath.Join(userDir, "projects.d")
	if err := os.MkdirAll(projectsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rootA := t.TempDir()
	writeProjectYAML(t, rootA, "legacy", "")
	if err := os.WriteFile(filepath.Join(projectsDir, "legacy.yaml"),
		[]byte("id: legacy\nroot: "+rootA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(userDir, "daemon.yaml")
	content := "projects_dir: " + projectsDir + "\nworkspaces:\n  - id: legacy\n    root: " + rootA + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(cfgPath); err == nil {
		t.Fatal("duplicate id across daemon.yaml and projects.d must be rejected")
	}
}

// TestLoadConfigProjectsDirMerge checks that an explicit projects_dir merges with
// daemon.yaml workspaces.
func TestLoadConfigProjectsDirMerge(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("MCP_GLOBAL_DIR", userDir)
	projectsDir := filepath.Join(userDir, "projects.d")
	if err := os.MkdirAll(projectsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeProjectYAML(t, rootB, "col-b", "")
	if err := os.WriteFile(filepath.Join(projectsDir, "col-b.yaml"),
		[]byte("id: col-b\nroot: "+rootB+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(userDir, "daemon.yaml")
	content := "projects_dir: " + projectsDir + "\nworkspaces:\n  - id: legacy\n    root: " + rootA + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 2 {
		t.Fatalf("workspaces=%v", cfg.Workspaces)
	}
}

func TestProjectManagerUserDirLayout(t *testing.T) {
	m := NewProjectManager(filepath.Join("root", "userdir"))
	if got := m.projectsDir(); !strings.HasSuffix(got, filepath.Join("userdir", "projects.d")) {
		t.Fatalf("projectsDir=%q", got)
	}
	if got := m.collectionsDir(); !strings.HasSuffix(got, filepath.Join("userdir", "data", "collections")) {
		t.Fatalf("collectionsDir=%q", got)
	}
	if got := m.registrationPath("abc"); !strings.HasSuffix(got, filepath.Join("projects.d", "abc.yaml")) {
		t.Fatalf("registrationPath=%q", got)
	}
}
