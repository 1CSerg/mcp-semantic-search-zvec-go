package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateCollectionID(t *testing.T) {
	valid := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"my-app",
		"my_app.v2",
		"a",
		"A1",
		strings.Repeat("x", maxCollectionIDLen),
	}
	for _, id := range valid {
		if err := ValidateCollectionID(id); err != nil {
			t.Fatalf("ValidateCollectionID(%q): %v", id, err)
		}
	}

	invalid := []string{
		"",
		"   ",
		"..",
		"a..b",
		"foo/bar",
		`foo\bar`,
		"/abs",
		".hidden",
		"-leading-dash",
		"with space",
		strings.Repeat("x", maxCollectionIDLen+1),
	}
	for _, id := range invalid {
		if err := ValidateCollectionID(id); err == nil {
			t.Fatalf("ValidateCollectionID(%q): expected error", id)
		}
	}
}

// TestProjectConfigSchema pins the project.yaml schema contract: an absent
// schema_version means 1 for backwards compatibility, other versions and any
// unknown key (top-level or nested) are rejected instead of silently ignored.
func TestProjectConfigSchema(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		wantErr     bool
		wantVersion int
	}{
		{
			name:        "absent version defaults to 1",
			content:     "collection_id: abc123\n",
			wantVersion: 1,
		},
		{
			name:        "explicit version 1",
			content:     "schema_version: 1\ncollection_id: abc123\n",
			wantVersion: 1,
		},
		{
			name:    "version 2 rejected",
			content: "schema_version: 2\ncollection_id: abc123\n",
			wantErr: true,
		},
		{
			name:    "negative version rejected",
			content: "schema_version: -1\ncollection_id: abc123\n",
			wantErr: true,
		},
		{
			name:    "unknown top-level key rejected",
			content: "collection_id: abc123\nmystery: value\n",
			wantErr: true,
		},
		{
			name:    "unknown indexing key rejected",
			content: "collection_id: abc123\nindexing:\n  mystery: 1\n",
			wantErr: true,
		},
		{
			name:    "unknown file_watcher key rejected",
			content: "collection_id: abc123\nfile_watcher:\n  mystery: true\n",
			wantErr: true,
		},
		{
			name:    "unknown language key rejected",
			content: "collection_id: abc123\nindexing:\n  chunking:\n    languages:\n      bsl:\n        mystery: true\n",
			wantErr: true,
		},
		{
			name:    "index path is not a project key",
			content: "collection_id: abc123\nindex_dir: /tmp/elsewhere\n",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ProjectFileName)
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			pc, err := LoadProjectConfig(path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", pc)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pc.SchemaVersion != tc.wantVersion {
				t.Fatalf("schema_version=%d want %d", pc.SchemaVersion, tc.wantVersion)
			}
		})
	}
}

// TestCollectionIDUnsafe pins the collection id contract: ids become directory
// names under the user data dir, so path separators, traversal, Windows device
// names and trailing dots are rejected.
func TestCollectionIDUnsafe(t *testing.T) {
	unsafe := []string{
		"",
		"   ",
		"../x",
		"a/b",
		`a\b`,
		`C:\x`,
		"C:/x",
		"..",
		"a..b",
		strings.Repeat("x", maxCollectionIDLen+1),
		"CON",
		"con",
		"nul",
		"COM1",
		"LPT9",
		"aux.txt",
		"abc.",
		".hidden",
	}
	for _, id := range unsafe {
		if err := ValidateCollectionID(id); err == nil {
			t.Fatalf("ValidateCollectionID(%q): expected error", id)
		}
	}

	safe := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"my-app",
		"my_app.v2",
		"console",
		"nulll",
		strings.Repeat("x", maxCollectionIDLen),
	}
	for _, id := range safe {
		if err := ValidateCollectionID(id); err != nil {
			t.Fatalf("ValidateCollectionID(%q): %v", id, err)
		}
	}
}

// TestProjectLanguageOverrideDecoding guards the pointer-based language decode:
// an override that only sets include_sdbl leaves enabled absent (nil) rather
// than collapsing to the Go zero value false.
func TestProjectLanguageOverrideDecoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), ProjectFileName)
	content := `collection_id: abc123
indexing:
  chunking:
    languages:
      bsl:
        include_sdbl: false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := LoadProjectConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if pc.Indexing == nil || pc.Indexing.Chunking == nil {
		t.Fatalf("chunking overrides missing: %+v", pc.Indexing)
	}
	lang, ok := pc.Indexing.Chunking.Languages["bsl"]
	if !ok {
		t.Fatalf("bsl override missing: %+v", pc.Indexing.Chunking.Languages)
	}
	if lang.Enabled != nil {
		t.Fatalf("enabled must stay absent (nil) when not set in the override: %v", *lang.Enabled)
	}
	if lang.IncludeSDBL == nil || *lang.IncludeSDBL {
		t.Fatalf("include_sdbl override not parsed: %+v", lang.IncludeSDBL)
	}
}

func TestLoadProjectConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ProjectFileName)
	content := `collection_id: "550e8400-e29b-41d4-a716-446655440000"
description: my-app
active_profile: fast
indexing:
  max_file_bytes: 1048576
file_watcher:
  enabled: true
  backend: polling
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := LoadProjectConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if pc.CollectionID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("collection_id=%q", pc.CollectionID)
	}
	if pc.Description != "my-app" || pc.ActiveProfile != "fast" {
		t.Fatalf("pc=%+v", pc)
	}
	if pc.Indexing == nil || pc.Indexing.MaxFileBytes == nil || *pc.Indexing.MaxFileBytes != 1048576 {
		t.Fatalf("indexing=%+v", pc.Indexing)
	}
	if pc.FileWatcher == nil || pc.FileWatcher.Enabled == nil || !*pc.FileWatcher.Enabled || pc.FileWatcher.Backend != "polling" {
		t.Fatalf("file_watcher=%+v", pc.FileWatcher)
	}
}

func TestLoadProjectConfigInvalidCollectionID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ProjectFileName)
	if err := os.WriteFile(path, []byte("collection_id: \"../escape\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjectConfig(path); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoadProjectConfigMissing(t *testing.T) {
	if _, err := LoadProjectConfig(filepath.Join(t.TempDir(), ProjectFileName)); err == nil {
		t.Fatal("expected error")
	}
}

func TestFindProjectConfig(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, DefaultInstallDirName)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projectDir, ProjectFileName)
	if err := os.WriteFile(path, []byte("collection_id: abc123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "sub", "dir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	foundPath, foundRoot, err := FindProjectConfig(nested)
	if err != nil {
		t.Fatal(err)
	}
	if foundPath != path {
		t.Fatalf("path=%q want %q", foundPath, path)
	}
	if foundRoot != root {
		t.Fatalf("root=%q want %q", foundRoot, root)
	}
}

func TestFindProjectConfigNotFound(t *testing.T) {
	if _, _, err := FindProjectConfig(t.TempDir()); err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyProjectOverrides(t *testing.T) {
	newApp := func() *AppConfig {
		app := &AppConfig{
			ActiveProfile: "base",
			Profiles: map[string]EmbeddingProfile{
				"base": {Provider: "openai_compatible", Dimensions: 384},
				"alt":  {Provider: "openai_compatible", Dimensions: 768},
			},
			Indexing: IndexingConfig{
				Extensions:   []string{".go"},
				SkipDirs:     []string{"vendor"},
				MaxFileBytes: 100,
				Chunking: ChunkingConfig{
					MinChunkTokens:    24,
					ProseOverlapRatio: 0.12,
					Languages: map[string]LanguageConfig{
						"go":     {Enabled: true},
						"python": {Enabled: true},
					},
				},
			},
			FileWatcher: FileWatcherConfig{Enabled: false, Backend: "auto", DebounceSeconds: 2, PollIntervalSeconds: 10},
		}
		return app
	}
	app := newApp()
	pc := &ProjectConfig{
		CollectionID:  "id1",
		ActiveProfile: "alt",
		Indexing: &ProjectIndexingOverrides{
			Extensions:   []string{".bsl", ".dcs"},
			MaxFileBytes: int64Ptr(500),
			Chunking: &ProjectChunkingOverrides{
				MinChunkTokens: intPtr(48),
				Languages: map[string]ProjectLanguageOverride{
					"bsl": {Enabled: boolPtr(true), IncludeSDBL: boolPtr(true)},
				},
			},
		},
		FileWatcher: &ProjectFileWatcherOverrides{
			Enabled: boolPtr(true),
			Backend: "polling",
		},
	}
	if err := ApplyProjectOverrides(app, pc); err != nil {
		t.Fatal(err)
	}

	if app.ActiveProfile != "alt" {
		t.Fatalf("active_profile=%q", app.ActiveProfile)
	}
	if len(app.Profiles) != 2 {
		t.Fatalf("profiles changed: %+v", app.Profiles)
	}
	if len(app.Indexing.Extensions) != 2 || app.Indexing.Extensions[0] != ".bsl" {
		t.Fatalf("extensions=%v", app.Indexing.Extensions)
	}
	if len(app.Indexing.SkipDirs) != 1 || app.Indexing.SkipDirs[0] != "vendor" {
		t.Fatalf("skip_dirs replaced unexpectedly: %v", app.Indexing.SkipDirs)
	}
	if app.Indexing.MaxFileBytes != 500 {
		t.Fatalf("max_file_bytes=%d", app.Indexing.MaxFileBytes)
	}
	if app.Indexing.Chunking.MinChunkTokens != 48 {
		t.Fatalf("min_chunk_tokens=%d", app.Indexing.Chunking.MinChunkTokens)
	}
	if app.Indexing.Chunking.ProseOverlapRatio != 0.12 {
		t.Fatalf("prose_overlap_ratio=%v", app.Indexing.Chunking.ProseOverlapRatio)
	}
	langs := app.Indexing.Chunking.Languages
	if len(langs) != 3 || !langs["go"].Enabled || !langs["python"].Enabled || !langs["bsl"].IncludeSDBL {
		t.Fatalf("languages=%+v", langs)
	}
	if !app.FileWatcher.Enabled || app.FileWatcher.Backend != "polling" {
		t.Fatalf("file_watcher=%+v", app.FileWatcher)
	}
	if app.FileWatcher.DebounceSeconds != 2 || app.FileWatcher.PollIntervalSeconds != 10 {
		t.Fatalf("file_watcher scalars changed: %+v", app.FileWatcher)
	}

	// Nil overrides leave the app config untouched except nothing at all.
	app = newApp()
	if err := ApplyProjectOverrides(app, &ProjectConfig{CollectionID: "id1"}); err != nil {
		t.Fatal(err)
	}
	if app.ActiveProfile != "base" || app.Indexing.MaxFileBytes != 100 {
		t.Fatalf("app=%+v", app)
	}

	if err := ApplyProjectOverrides(newApp(), &ProjectConfig{CollectionID: "id1", ActiveProfile: "missing"}); err == nil {
		t.Fatal("expected error for unknown active_profile")
	}
}

func TestUserDataDirGlobalOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envGlobalDir, dir)

	got, err := UserDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(dir) {
		t.Fatalf("UserDataDir=%q want %q", got, dir)
	}

	cfgPath, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if cfgPath != filepath.Join(dir, "config.yaml") {
		t.Fatalf("GlobalConfigPath=%q", cfgPath)
	}
	collDir, err := CollectionDir("abc123")
	if err != nil {
		t.Fatal(err)
	}
	if collDir != filepath.Join(dir, "data", "collections", "abc123") {
		t.Fatalf("CollectionDir=%q", collDir)
	}
	if _, err := CollectionDir("../bad"); err == nil {
		t.Fatal("expected error for unsafe collection id")
	}
	projDir, err := ProjectsDir()
	if err != nil {
		t.Fatal(err)
	}
	if projDir != filepath.Join(dir, "projects.d") {
		t.Fatalf("ProjectsDir=%q", projDir)
	}
}

func TestLoadForCollection(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv(envGlobalDir, globalDir)

	globalConfig := `active_profile: base
profiles:
  base:
    provider: openai_compatible
    model: m
    dimensions: 384
    base_url: http://127.0.0.1:9/v1
    api_key_env: TEST_GLOBAL_API_KEY
`
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, ".env"), []byte("TEST_GLOBAL_API_KEY=global-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspace := t.TempDir()
	pc := &ProjectConfig{
		CollectionID: "550e8400-e29b-41d4-a716-446655440000",
		Indexing: &ProjectIndexingOverrides{
			SkipDirs: []string{"node_modules"},
		},
	}

	settings, err := LoadForCollection(pc, workspace, LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}

	wantIndex := filepath.Join(globalDir, "data", "collections", pc.CollectionID)
	if settings.IndexDir != wantIndex {
		t.Fatalf("index_dir=%q want %q", settings.IndexDir, wantIndex)
	}
	if settings.ConfigPath != filepath.Join(globalDir, "config.yaml") {
		t.Fatalf("config_path=%q", settings.ConfigPath)
	}
	if settings.WorkspaceID != pc.CollectionID || settings.CollectionID != pc.CollectionID {
		t.Fatalf("workspace_id=%q collection_id=%q", settings.WorkspaceID, settings.CollectionID)
	}
	if settings.WorkspaceRoot != NormalizeAbsolutePath(workspace) {
		t.Fatalf("workspace_root=%q", settings.WorkspaceRoot)
	}
	profile, err := settings.ActiveProfile()
	if err != nil {
		t.Fatal(err)
	}
	if profile.APIKey != "global-secret" {
		t.Fatalf("api_key=%q want secret from global .env", profile.APIKey)
	}
	if len(settings.App.Indexing.SkipDirs) != 1 || settings.App.Indexing.SkipDirs[0] != "node_modules" {
		t.Fatalf("skip_dirs=%v", settings.App.Indexing.SkipDirs)
	}
}

func TestLoadForCollectionInvalidID(t *testing.T) {
	t.Setenv(envGlobalDir, t.TempDir())
	if _, err := LoadForCollection(&ProjectConfig{CollectionID: "../bad"}, t.TempDir(), LoadOptions{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := LoadForCollection(nil, t.TempDir(), LoadOptions{}); err == nil {
		t.Fatal("expected error for nil project config")
	}
}
