package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProjectFileName is the per-project config file inside the install marker dir.
const ProjectFileName = "project.yaml"

const maxCollectionIDLen = 64

// ProjectSchemaVersion is the only supported project.yaml schema version.
const ProjectSchemaVersion = 1

var collectionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// windowsDeviceNames are reserved DOS device names. A file or directory whose
// name (stem, before the first dot) matches one of them is not addressable on
// Windows, so a collection id must not collide with them.
var windowsDeviceNames = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {},
	"com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {},
	"lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

// ProjectConfig is the per-project project.yaml in the global-install layout.
//
// The file is portable: it never stores absolute paths, index locations, daemon
// endpoints or secrets. Those are derived from the user-level install.
type ProjectConfig struct {
	// SchemaVersion is 1 for current files. An absent key means 1 for
	// compatibility with files written before versioning existed.
	SchemaVersion int    `yaml:"schema_version"`
	CollectionID  string `yaml:"collection_id"`
	Description   string `yaml:"description,omitempty"`
	ActiveProfile string `yaml:"active_profile,omitempty"`
	// DaemonURL is read-only legacy compatibility: generated configs never write
	// it and the new default mode never dials an arbitrary URL from a project.
	DaemonURL   string                       `yaml:"daemon_url,omitempty"`
	Indexing    *ProjectIndexingOverrides    `yaml:"indexing,omitempty"`
	FileWatcher *ProjectFileWatcherOverrides `yaml:"file_watcher,omitempty"`
}

// ProjectIndexingOverrides are optional per-project indexing overrides.
// Slices replace the global value entirely when set; Languages merges per key.
type ProjectIndexingOverrides struct {
	Extensions   []string                  `yaml:"extensions,omitempty"`
	SkipDirs     []string                  `yaml:"skip_dirs,omitempty"`
	MaxFileBytes *int64                    `yaml:"max_file_bytes,omitempty"`
	Chunking     *ProjectChunkingOverrides `yaml:"chunking,omitempty"`
}

// ProjectChunkingOverrides are optional per-project chunking overrides.
// Only defaults may be overridden per project; strategy, version and
// line-window geometry stay global so index identity cannot drift per project.
type ProjectChunkingOverrides struct {
	MinChunkTokens    *int                               `yaml:"min_chunk_tokens,omitempty"`
	ProseOverlapRatio *float64                           `yaml:"prose_overlap_ratio,omitempty"`
	Languages         map[string]ProjectLanguageOverride `yaml:"languages,omitempty"`
}

// ProjectLanguageOverride is a partial language override: nil fields are absent
// in the project file and keep the global value, so "include_sdbl: false" does
// not silently disable the language itself.
type ProjectLanguageOverride struct {
	Enabled     *bool `yaml:"enabled,omitempty"`
	IncludeSDBL *bool `yaml:"include_sdbl,omitempty"`
}

// ProjectFileWatcherOverrides are optional per-project file watcher overrides.
type ProjectFileWatcherOverrides struct {
	Enabled             *bool    `yaml:"enabled,omitempty"`
	Backend             string   `yaml:"backend,omitempty"`
	DebounceSeconds     *float64 `yaml:"debounce_seconds,omitempty"`
	PollIntervalSeconds *float64 `yaml:"poll_interval_seconds,omitempty"`
}

// ValidateCollectionID checks that id is safe to use as a directory name.
func ValidateCollectionID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("collection_id is required")
	}
	if len(id) > maxCollectionIDLen {
		return fmt.Errorf("collection_id %q is too long (max %d chars)", id, maxCollectionIDLen)
	}
	if !collectionIDPattern.MatchString(id) {
		return fmt.Errorf("collection_id %q must match [A-Za-z0-9][A-Za-z0-9._-]*", id)
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("collection_id %q must not contain '..'", id)
	}
	if strings.HasSuffix(id, ".") {
		return fmt.Errorf("collection_id %q must not end with '.'", id)
	}
	stem, _, _ := strings.Cut(id, ".")
	if _, reserved := windowsDeviceNames[strings.ToLower(stem)]; reserved {
		return fmt.Errorf("collection_id %q is a reserved device name", id)
	}
	return nil
}

// ProjectConfigPath returns the project.yaml path under a workspace root.
func ProjectConfigPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, DefaultInstallDirName, ProjectFileName)
}

// LoadProjectConfig parses and validates project.yaml with a strict schema:
// unknown keys and unsupported schema versions are errors, never ignored.
func LoadProjectConfig(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read project config %s: %w", path, err)
	}
	pc, err := ParseProjectConfig(data)
	if err != nil {
		return nil, fmt.Errorf("project config %s: %w", path, err)
	}
	return pc, nil
}

// ParseProjectConfig decodes project.yaml bytes strictly. Split out from
// LoadProjectConfig so callers can validate a candidate snapshot before writing.
func ParseProjectConfig(data []byte) (*ProjectConfig, error) {
	var pc ProjectConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&pc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("empty project config")
		}
		return nil, fmt.Errorf("parse: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("parse: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("parse: %w", err)
	}

	switch pc.SchemaVersion {
	case 0:
		// Absent schema_version: legacy file, treat as version 1.
		pc.SchemaVersion = ProjectSchemaVersion
	case ProjectSchemaVersion:
	default:
		return nil, fmt.Errorf("schema_version %d is not supported (want %d)", pc.SchemaVersion, ProjectSchemaVersion)
	}
	if err := ValidateCollectionID(pc.CollectionID); err != nil {
		return nil, err
	}
	return &pc, nil
}

// MarshalProjectConfig renders a project.yaml with an explicit schema_version.
func MarshalProjectConfig(pc *ProjectConfig) ([]byte, error) {
	if pc == nil {
		return nil, fmt.Errorf("project config is required")
	}
	out := *pc
	if out.SchemaVersion == 0 {
		out.SchemaVersion = ProjectSchemaVersion
	}
	// Legacy field: never written by generated configs.
	out.DaemonURL = ""
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&out); err != nil {
		return nil, fmt.Errorf("encode project config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode project config: %w", err)
	}
	return buf.Bytes(), nil
}

// FindProjectConfig walks up from startDir looking for
// .mcp-semantic-search-zvec-go/project.yaml. Returns the file path and the
// workspace root (two levels above the file). The root is never stored in the
// file so moving a project does not break its identity.
func FindProjectConfig(startDir string) (path string, root string, err error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve start dir: %w", err)
	}
	for {
		candidate := ProjectConfigPath(dir)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("no %s found at or above %s", filepath.Join(DefaultInstallDirName, ProjectFileName), startDir)
		}
		dir = parent
	}
}

// ApplyProjectOverrides merges per-project overrides on top of the global app config.
// Profiles are global and cannot be overridden from a project.
//
// The merge is atomic and non-aliasing: it validates the project overrides
// first, works on a deep copy of app, and only then swaps the result in, so a
// rejected override leaves app untouched and the merged config shares no maps
// or slices with the project config.
func ApplyProjectOverrides(app *AppConfig, pc *ProjectConfig) error {
	if app == nil || pc == nil {
		return nil
	}
	if err := validateProjectOverrides(pc); err != nil {
		return err
	}
	if pc.ActiveProfile != "" {
		if _, ok := app.Profiles[pc.ActiveProfile]; !ok {
			return fmt.Errorf("project config: active_profile %q not found in global profiles", pc.ActiveProfile)
		}
	}
	merged := cloneAppConfig(*app)
	applyProjectOverrides(&merged, pc)
	*app = merged
	return nil
}

// applyProjectOverrides mutates a private copy produced by cloneAppConfig.
func applyProjectOverrides(app *AppConfig, pc *ProjectConfig) {
	if pc.ActiveProfile != "" {
		app.ActiveProfile = pc.ActiveProfile
	}
	if idx := pc.Indexing; idx != nil {
		if idx.Extensions != nil {
			app.Indexing.Extensions = cloneStrings(idx.Extensions)
		}
		if idx.SkipDirs != nil {
			app.Indexing.SkipDirs = cloneStrings(idx.SkipDirs)
		}
		if idx.MaxFileBytes != nil {
			app.Indexing.MaxFileBytes = *idx.MaxFileBytes
		}
		if ch := idx.Chunking; ch != nil {
			if ch.MinChunkTokens != nil {
				app.Indexing.Chunking.MinChunkTokens = *ch.MinChunkTokens
			}
			if ch.ProseOverlapRatio != nil {
				app.Indexing.Chunking.ProseOverlapRatio = *ch.ProseOverlapRatio
			}
			if len(ch.Languages) > 0 {
				if app.Indexing.Chunking.Languages == nil {
					app.Indexing.Chunking.Languages = make(map[string]LanguageConfig, len(ch.Languages))
				}
				for name, override := range ch.Languages {
					merged := app.Indexing.Chunking.Languages[name]
					if override.Enabled != nil {
						merged.Enabled = *override.Enabled
					}
					if override.IncludeSDBL != nil {
						merged.IncludeSDBL = *override.IncludeSDBL
					}
					app.Indexing.Chunking.Languages[name] = merged
				}
			}
		}
	}
	if fw := pc.FileWatcher; fw != nil {
		if fw.Enabled != nil {
			app.FileWatcher.Enabled = *fw.Enabled
		}
		if fw.Backend != "" {
			app.FileWatcher.Backend = fw.Backend
		}
		if fw.DebounceSeconds != nil {
			app.FileWatcher.DebounceSeconds = *fw.DebounceSeconds
		}
		if fw.PollIntervalSeconds != nil {
			app.FileWatcher.PollIntervalSeconds = *fw.PollIntervalSeconds
		}
	}
}
