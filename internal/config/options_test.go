package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

func f64Ptr(v float64) *float64 { return &v }

func int64Ptr(v int64) *int64 { return &v }

// collectionFixture writes a global install layout (config.yaml + optional .env)
// under a temporary MCP_GLOBAL_DIR and returns that directory.
func collectionFixture(t *testing.T, configYAML, envContent string) string {
	t.Helper()
	globalDir := t.TempDir()
	t.Setenv(envGlobalDir, globalDir)
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if envContent != "" {
		if err := os.WriteFile(filepath.Join(globalDir, ".env"), []byte(envContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return globalDir
}

const collectionGlobalConfig = `active_profile: base
profiles:
  base:
    provider: openai_compatible
    model: m
    dimensions: 384
    base_url: http://127.0.0.1:9/v1
    api_key_env: TEST_COLLECTION_KEY
  keyless:
    provider: onnx
    model: onnx-model
    dimensions: 384
    max_input_tokens: 256
indexing:
  extensions: [".go", ".md"]
  skip_dirs: [".git", "vendor"]
  max_file_bytes: 1048576
  chunking:
    min_chunk_tokens: 24
    prose_overlap_ratio: 0.12
    languages:
      go:
        enabled: true
      bsl:
        enabled: true
        include_sdbl: true
file_watcher:
  enabled: false
  backend: auto
`

// TestCollectionSettingsIsolation proves that applying one collection's
// overrides cannot leak into another collection loaded from the same global
// config, including when both loads run concurrently under the race detector.
func TestCollectionSettingsIsolation(t *testing.T) {
	collectionFixture(t, collectionGlobalConfig, "TEST_COLLECTION_KEY=global-secret\n")

	pcA := &ProjectConfig{
		CollectionID: "collection-a",
		Indexing: &ProjectIndexingOverrides{
			Extensions: []string{".bsl"},
			Chunking: &ProjectChunkingOverrides{
				Languages: map[string]ProjectLanguageOverride{
					"go":  {Enabled: boolPtr(false)},
					"bsl": {IncludeSDBL: boolPtr(false)},
				},
			},
		},
		FileWatcher: &ProjectFileWatcherOverrides{Enabled: boolPtr(true)},
	}
	pcB := &ProjectConfig{CollectionID: "collection-b"}

	// Load B first so it holds the pristine global values, then A, then B again.
	before, err := LoadForCollection(pcB, t.TempDir(), LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	settingsA, err := LoadForCollection(pcA, t.TempDir(), LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := LoadForCollection(pcB, t.TempDir(), LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got := settingsA.App.Indexing.Extensions; len(got) != 1 || got[0] != ".bsl" {
		t.Fatalf("A extensions=%v", got)
	}
	if !settingsA.App.FileWatcher.Enabled {
		t.Fatal("A file_watcher.enabled should be true")
	}
	if settingsA.App.Indexing.Chunking.Languages["go"].Enabled {
		t.Fatal("A go language should be disabled")
	}
	if settingsA.App.Indexing.Chunking.Languages["bsl"].IncludeSDBL {
		t.Fatal("A bsl include_sdbl should be false")
	}
	if !settingsA.App.Indexing.Chunking.Languages["bsl"].Enabled {
		t.Fatal("A bsl enabled must stay true (absent in override)")
	}

	for name, settings := range map[string]*Settings{"before": before, "after": after} {
		if got := settings.App.Indexing.Extensions; len(got) != 2 || got[0] != ".go" {
			t.Fatalf("%s extensions leaked: %v", name, got)
		}
		if settings.App.FileWatcher.Enabled {
			t.Fatalf("%s file_watcher.enabled leaked", name)
		}
		if !settings.App.Indexing.Chunking.Languages["go"].Enabled {
			t.Fatalf("%s go language leaked", name)
		}
		if !settings.App.Indexing.Chunking.Languages["bsl"].IncludeSDBL {
			t.Fatalf("%s bsl include_sdbl leaked", name)
		}
		if settings.App.Indexing.Chunking.MinChunkTokens != 24 {
			t.Fatalf("%s min_chunk_tokens leaked: %d", name, settings.App.Indexing.Chunking.MinChunkTokens)
		}
	}

	// The project config itself must not be mutated by the merge.
	if len(pcA.Indexing.Extensions) != 1 || pcA.Indexing.Extensions[0] != ".bsl" {
		t.Fatalf("pcA mutated: %v", pcA.Indexing.Extensions)
	}
	if pcA.Indexing.Chunking.Languages["bsl"].Enabled != nil {
		t.Fatal("pcA language override mutated")
	}

	// Concurrent loads of A and B must be race-free and independent.
	t.Run("parallel", func(t *testing.T) {
		done := make(chan error, 2)
		for _, pc := range []*ProjectConfig{pcA, pcB} {
			go func(pc *ProjectConfig) {
				for i := 0; i < 4; i++ {
					if _, err := LoadForCollection(pc, t.TempDir(), LoadOptions{}); err != nil {
						done <- err
						return
					}
				}
				done <- nil
			}(pc)
		}
		for i := 0; i < 2; i++ {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
	})
}

// TestProjectPartialLanguageOverride checks the merge keeps global fields that
// the project did not mention, for both directions of the optional bools.
func TestProjectPartialLanguageOverride(t *testing.T) {
	collectionFixture(t, collectionGlobalConfig, "TEST_COLLECTION_KEY=global-secret\n")

	pc := &ProjectConfig{
		CollectionID: "collection-bsl",
		Indexing: &ProjectIndexingOverrides{
			Chunking: &ProjectChunkingOverrides{
				Languages: map[string]ProjectLanguageOverride{
					"bsl": {IncludeSDBL: boolPtr(false)},
				},
			},
		},
	}
	settings, err := LoadForCollection(pc, t.TempDir(), LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	bsl := settings.App.Indexing.Chunking.Languages["bsl"]
	if !bsl.Enabled {
		t.Fatal("bsl enabled=true from global config must survive a partial override")
	}
	if bsl.IncludeSDBL {
		t.Fatal("bsl include_sdbl=false override not applied")
	}
	if !settings.App.Indexing.Chunking.Languages["go"].Enabled {
		t.Fatal("unrelated language go must stay enabled")
	}
}

// TestProjectSecretIsolation proves secrets come only from the global install:
// a project .env is never read in the new mode.
func TestProjectSecretIsolation(t *testing.T) {
	globalDir := collectionFixture(t, collectionGlobalConfig, "TEST_COLLECTION_KEY=global-secret\n")

	projectRoot := t.TempDir()
	installDir := filepath.Join(projectRoot, DefaultInstallDirName)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A project-local .env must be ignored even though it sits in the legacy location.
	if err := os.WriteFile(filepath.Join(installDir, ".env"), []byte("TEST_COLLECTION_KEY=project-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	settings, err := LoadForCollection(&ProjectConfig{CollectionID: "collection-secret"}, projectRoot, LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := settings.ActiveProfile()
	if err != nil {
		t.Fatal(err)
	}
	if profile.APIKey != "global-secret" {
		t.Fatalf("api_key=%q, project .env must not be read", profile.APIKey)
	}

	// A profile that needs no key works without any .env file at all.
	if err := os.Remove(filepath.Join(globalDir, ".env")); err != nil {
		t.Fatal(err)
	}
	keyless := &ProjectConfig{CollectionID: "collection-keyless", ActiveProfile: "keyless"}
	settings, err = LoadForCollection(keyless, t.TempDir(), LoadOptions{})
	if err != nil {
		t.Fatalf("keyless profile without .env must load: %v", err)
	}
	profile, err = settings.ActiveProfile()
	if err != nil {
		t.Fatal(err)
	}
	if profile.APIKey != "" {
		t.Fatalf("keyless api_key=%q", profile.APIKey)
	}

	// A profile that names api_key_env with no matching secret is an explicit error.
	if _, err := LoadForCollection(&ProjectConfig{CollectionID: "collection-secret"}, t.TempDir(), LoadOptions{}); err == nil {
		t.Fatal("expected an explicit error when api_key_env has no secret")
	} else if !strings.Contains(err.Error(), "TEST_COLLECTION_KEY") {
		t.Fatalf("error should name the missing secret key: %v", err)
	}
}

// TestProjectProfileValidation checks that invalid merged settings are rejected
// after overrides are applied, while explicit zero/false/empty values survive.
func TestProjectProfileValidation(t *testing.T) {
	collectionFixture(t, collectionGlobalConfig, "TEST_COLLECTION_KEY=global-secret\n")

	cases := []struct {
		name string
		pc   *ProjectConfig
	}{
		{
			name: "unknown profile",
			pc:   &ProjectConfig{CollectionID: "c1", ActiveProfile: "nope"},
		},
		{
			name: "prose overlap above one",
			pc: &ProjectConfig{CollectionID: "c1", Indexing: &ProjectIndexingOverrides{
				Chunking: &ProjectChunkingOverrides{ProseOverlapRatio: f64Ptr(1.5)},
			}},
		},
		{
			name: "negative prose overlap",
			pc: &ProjectConfig{CollectionID: "c1", Indexing: &ProjectIndexingOverrides{
				Chunking: &ProjectChunkingOverrides{ProseOverlapRatio: f64Ptr(-0.1)},
			}},
		},
		{
			name: "negative min chunk tokens",
			pc: &ProjectConfig{CollectionID: "c1", Indexing: &ProjectIndexingOverrides{
				Chunking: &ProjectChunkingOverrides{MinChunkTokens: intPtr(-1)},
			}},
		},
		{
			name: "negative max file bytes",
			pc: &ProjectConfig{CollectionID: "c1", Indexing: &ProjectIndexingOverrides{
				MaxFileBytes: int64Ptr(-5),
			}},
		},
		{
			name: "unknown watcher backend",
			pc: &ProjectConfig{CollectionID: "c1", FileWatcher: &ProjectFileWatcherOverrides{
				Backend: "magic",
			}},
		},
		{
			name: "negative debounce",
			pc: &ProjectConfig{CollectionID: "c1", FileWatcher: &ProjectFileWatcherOverrides{
				DebounceSeconds: f64Ptr(-1),
			}},
		},
		{
			name: "empty extension entry",
			pc: &ProjectConfig{CollectionID: "c1", Indexing: &ProjectIndexingOverrides{
				Extensions: []string{".go", "  "},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := LoadForCollection(tc.pc, t.TempDir(), LoadOptions{}); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}

	// Explicit zero/false/empty values must be preserved, not treated as absent.
	pc := &ProjectConfig{
		CollectionID: "collection-explicit",
		Indexing: &ProjectIndexingOverrides{
			Extensions: []string{},
			SkipDirs:   []string{},
			Chunking: &ProjectChunkingOverrides{
				MinChunkTokens:    intPtr(0),
				ProseOverlapRatio: f64Ptr(0),
				Languages: map[string]ProjectLanguageOverride{
					"go": {Enabled: boolPtr(false)},
				},
			},
		},
		FileWatcher: &ProjectFileWatcherOverrides{Enabled: boolPtr(false)},
	}
	settings, err := LoadForCollection(pc, t.TempDir(), LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := settings.App.Indexing.Extensions; len(got) != 0 {
		t.Fatalf("explicit empty extensions lost: %v", got)
	}
	if got := settings.App.Indexing.SkipDirs; len(got) != 0 {
		t.Fatalf("explicit empty skip_dirs lost: %v", got)
	}
	if settings.App.Indexing.Chunking.MinChunkTokens != 0 {
		t.Fatalf("explicit zero min_chunk_tokens lost: %d", settings.App.Indexing.Chunking.MinChunkTokens)
	}
	if settings.App.Indexing.Chunking.ProseOverlapRatio != 0 {
		t.Fatalf("explicit zero prose_overlap_ratio lost: %v", settings.App.Indexing.Chunking.ProseOverlapRatio)
	}
	if settings.App.Indexing.Chunking.Languages["go"].Enabled {
		t.Fatal("explicit false language override lost")
	}
	if settings.App.FileWatcher.Enabled {
		t.Fatal("explicit false file_watcher.enabled lost")
	}
}

// TestCollectionLogsDirPointsAtDaemonLogs: a collection has no per-project install
// tree, so its diagnostics must name the directory the daemon actually logs to.
func TestCollectionLogsDirPointsAtDaemonLogs(t *testing.T) {
	globalDir := collectionFixture(t, collectionGlobalConfig, "TEST_COLLECTION_KEY=global-secret\n")

	settings, err := LoadForCollection(&ProjectConfig{CollectionID: "collection-logs"}, t.TempDir(), LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := GlobalLogsDir()
	if err != nil {
		t.Fatal(err)
	}
	if got := settings.LogsDir(); got != want {
		t.Fatalf("LogsDir=%q want %q", got, want)
	}
	if want != filepath.Join(globalDir, "data", "logs") {
		t.Fatalf("GlobalLogsDir=%q want <userdir>/data/logs", want)
	}
}

func TestLoadWithOptionsExplicitPaths(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	indexDir := filepath.Join(dir, "custom", "index")

	settings, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot: dir,
		WorkspaceID:   "ws-test",
		IndexDir:      indexDir,
		ConfigPath:    path,
		UseProcessEnv: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.WorkspaceID != "ws-test" {
		t.Fatalf("workspace_id=%q", settings.WorkspaceID)
	}
	wantIndex := filepath.Join(dir, "custom", "index")
	if settings.IndexDir != wantIndex {
		t.Fatalf("index_dir=%q want %q", settings.IndexDir, wantIndex)
	}
}

func TestLoadWithOptionsPerWorkspaceEnvNoGlobalLeak(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)
	configPath = filepath.Join(dir, "config.yaml")
	profileYAML := `active_profile: secret_profile
profiles:
  secret_profile:
    provider: openai_compatible
    model: m
    dimensions: 384
    base_url: http://127.0.0.1:9/v1
    api_key_env: ROUTERAI_API_KEY
`
	if err := os.WriteFile(configPath, []byte(profileYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("ROUTERAI_API_KEY=workspace-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ROUTERAI_API_KEY", "")

	settings, err := LoadWithOptions(LoadOptions{
		WorkspaceRoot: dir,
		ConfigPath:    configPath,
		UseProcessEnv: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := settings.ActiveProfile()
	if err != nil {
		t.Fatal(err)
	}
	if profile.APIKey != "workspace-secret" {
		t.Fatalf("api_key=%q want workspace-secret", profile.APIKey)
	}
	if got := os.Getenv("ROUTERAI_API_KEY"); got != "" {
		t.Fatalf("process env leaked: ROUTERAI_API_KEY=%q", got)
	}
}

func TestLoadWorkspaceFromSpec(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	settings, err := LoadWorkspaceFromSpec("my-app", dir, filepath.Join(dir, "idx"), path, LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if settings.WorkspaceID != "my-app" {
		t.Fatalf("workspace_id=%q", settings.WorkspaceID)
	}
}

func TestParseDotEnvMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("KEY=value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ParseDotEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if m["KEY"] != "value" {
		t.Fatalf("KEY=%q", m["KEY"])
	}
}

func TestWarnPlaintextAPIKeys(t *testing.T) {
	app := AppConfig{
		Profiles: map[string]EmbeddingProfile{
			"leaky": {Provider: "openai_compatible", APIKey: "hardcoded"},
			"safe":  {Provider: "openai_compatible", APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
	warnPlaintextAPIKeys(&app, "/tmp/config.yaml")
}

func TestDotEnvCandidatePathsWithEnvPath(t *testing.T) {
	paths := dotEnvCandidatePaths("/ws", "/ws/config.yaml", "/custom/.env")
	if len(paths) != 3 {
		t.Fatalf("paths=%v", paths)
	}
	if paths[0] != filepath.Join(filepath.Dir("/ws/config.yaml"), ".env") {
		t.Fatalf("paths[0]=%q", paths[0])
	}
	if paths[len(paths)-1] != "/custom/.env" {
		t.Fatalf("paths last=%q", paths[len(paths)-1])
	}
}

func TestCloneSecretsEmpty(t *testing.T) {
	if cloneSecrets(nil) != nil {
		t.Fatal("expected nil for nil input")
	}
	if cloneSecrets(map[string]string{}) != nil {
		t.Fatal("expected nil for empty map")
	}
}

func TestMergeDotEnvIntoMapParseError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, ".env")
	if err := os.WriteFile(bad, []byte("INVALID LINE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := mergeDotEnvIntoMap(map[string]string{}, []string{bad})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestApplyProfileSecretsSkipsPlaintextKey(t *testing.T) {
	app := AppConfig{
		Profiles: map[string]EmbeddingProfile{
			"p": {APIKey: "set", APIKeyEnv: "OTHER"},
		},
	}
	applyProfileSecrets(&app, map[string]string{"OTHER": "from-env"}, false)
	if app.Profiles["p"].APIKey != "set" {
		t.Fatal("should not overwrite plaintext api_key")
	}
}

func TestLookupSecretFromMapAndEnv(t *testing.T) {
	if got := lookupSecret(map[string]string{"K": "from-map"}, "K", false); got != "from-map" {
		t.Fatalf("got=%q", got)
	}
	t.Setenv("ENV_KEY", "from-env")
	if got := lookupSecret(nil, "ENV_KEY", true); got != "from-env" {
		t.Fatalf("got=%q", got)
	}
	if got := lookupSecret(map[string]string{}, "MISSING", true); got != "" {
		t.Fatalf("got=%q", got)
	}
}

func TestLoadWorkspaceFromSpecRequiresID(t *testing.T) {
	_, err := LoadWorkspaceFromSpec("", t.TempDir(), "", "", LoadOptions{})
	if err == nil || !strings.Contains(err.Error(), "workspace id is required") {
		t.Fatalf("err=%v", err)
	}
}
