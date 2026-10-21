package config

import (
	"fmt"
	"strings"
)

// This file holds the collection-settings merge rules for the global install:
// built-in defaults -> global config.yaml -> allowed project overrides -> final
// validation. Two invariants matter and are both enforced here:
//
//   - The merged AppConfig is a fresh, independent object. Nothing in it may
//     alias a map or slice owned by the shared project config or by another
//     collection's settings, so one project can never mutate another's view.
//   - Nothing outside the allowlist in section 3.3 of the plan can be set from a
//     project: no provider/model/dimensions/base_url, no chunking strategy or
//     version, no index/model/log paths, no daemon listener or token.

// cloneAppConfig deep-copies an AppConfig so a merge cannot mutate shared maps
// or slices (profiles, language maps, extension lists).
func cloneAppConfig(in AppConfig) AppConfig {
	out := in
	if in.Profiles != nil {
		out.Profiles = make(map[string]EmbeddingProfile, len(in.Profiles))
		for name, p := range in.Profiles {
			out.Profiles[name] = cloneEmbeddingProfile(p)
		}
	}
	out.Indexing.Extensions = cloneStrings(in.Indexing.Extensions)
	out.Indexing.SkipDirs = cloneStrings(in.Indexing.SkipDirs)
	if in.Indexing.Chunking.ContextPrefix != nil {
		v := *in.Indexing.Chunking.ContextPrefix
		out.Indexing.Chunking.ContextPrefix = &v
	}
	if in.Indexing.Chunking.Languages != nil {
		out.Indexing.Chunking.Languages = make(map[string]LanguageConfig, len(in.Indexing.Chunking.Languages))
		for name, lang := range in.Indexing.Chunking.Languages {
			out.Indexing.Chunking.Languages[name] = lang
		}
	}
	return out
}

func cloneEmbeddingProfile(p EmbeddingProfile) EmbeddingProfile {
	if p.ExtraHeaders != nil {
		headers := make(map[string]string, len(p.ExtraHeaders))
		for k, v := range p.ExtraHeaders {
			headers[k] = v
		}
		p.ExtraHeaders = headers
	}
	return p
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// validateProjectOverrides validates a project config on its own, before any
// merge, so a broken file fails with a message naming the project field.
func validateProjectOverrides(pc *ProjectConfig) error {
	if pc == nil {
		return fmt.Errorf("project config is required")
	}
	if err := ValidateCollectionID(pc.CollectionID); err != nil {
		return err
	}
	if idx := pc.Indexing; idx != nil {
		if idx.MaxFileBytes != nil && *idx.MaxFileBytes < 0 {
			return fmt.Errorf("project config: indexing.max_file_bytes must not be negative")
		}
		for _, ext := range idx.Extensions {
			if strings.TrimSpace(ext) == "" {
				return fmt.Errorf("project config: indexing.extensions must not contain empty entries")
			}
		}
		for _, dir := range idx.SkipDirs {
			if strings.TrimSpace(dir) == "" {
				return fmt.Errorf("project config: indexing.skip_dirs must not contain empty entries")
			}
		}
		if ch := idx.Chunking; ch != nil {
			if ch.MinChunkTokens != nil && *ch.MinChunkTokens < 0 {
				return fmt.Errorf("project config: indexing.chunking.min_chunk_tokens must not be negative")
			}
			if ch.ProseOverlapRatio != nil {
				r := *ch.ProseOverlapRatio
				if r < 0 || r >= 1 {
					return fmt.Errorf("project config: indexing.chunking.prose_overlap_ratio must be in [0, 1), got %v", r)
				}
			}
			for name := range ch.Languages {
				if strings.TrimSpace(name) == "" {
					return fmt.Errorf("project config: indexing.chunking.languages has an empty language name")
				}
			}
		}
	}
	if fw := pc.FileWatcher; fw != nil {
		if fw.Backend != "" {
			switch strings.ToLower(strings.TrimSpace(fw.Backend)) {
			case "auto", "inotify", "polling":
			default:
				return fmt.Errorf("project config: file_watcher.backend %q must be auto, inotify or polling", fw.Backend)
			}
		}
		if fw.DebounceSeconds != nil && *fw.DebounceSeconds < 0 {
			return fmt.Errorf("project config: file_watcher.debounce_seconds must not be negative")
		}
		if fw.PollIntervalSeconds != nil && *fw.PollIntervalSeconds < 0 {
			return fmt.Errorf("project config: file_watcher.poll_interval_seconds must not be negative")
		}
	}
	return nil
}

// ValidateCollectionSettings is the final validation for a collection's merged
// settings. It runs after ApplyProjectOverrides so it sees exactly the values
// the collection will use, and it must not mutate the settings.
func ValidateCollectionSettings(settings *Settings) error {
	if settings == nil {
		return fmt.Errorf("settings are required")
	}
	if strings.TrimSpace(settings.WorkspaceRoot) == "" {
		return fmt.Errorf("workspace root is required")
	}
	if err := ValidateCollectionID(settings.WorkspaceID); err != nil {
		return fmt.Errorf("workspace id: %w", err)
	}

	app := &settings.App
	if strings.TrimSpace(app.ActiveProfile) == "" {
		return fmt.Errorf("active_profile is not set")
	}
	profile, err := settings.ActiveProfile()
	if err != nil {
		return err
	}
	if strings.TrimSpace(profile.APIKeyEnv) != "" && strings.TrimSpace(profile.APIKey) == "" {
		return fmt.Errorf("profile %q: api_key_env %q is not set in the global .env", app.ActiveProfile, profile.APIKeyEnv)
	}

	idx := app.Indexing
	if idx.MaxFileBytes <= 0 {
		return fmt.Errorf("indexing.max_file_bytes must be positive, got %d", idx.MaxFileBytes)
	}
	if idx.StreamChunkThresholdBytes < 0 {
		return fmt.Errorf("indexing.stream_chunk_threshold_bytes must not be negative")
	}
	if idx.MaxLineBytes < 0 {
		return fmt.Errorf("indexing.max_line_bytes must not be negative")
	}
	if idx.LockStaleSeconds < 0 || idx.StallSeconds < 0 || idx.HeartbeatSeconds < 0 {
		return fmt.Errorf("indexing timing values must not be negative")
	}
	for _, ext := range idx.Extensions {
		if strings.TrimSpace(ext) == "" {
			return fmt.Errorf("indexing.extensions must not contain empty entries")
		}
	}
	ch := idx.Chunking
	if ch.MinChunkTokens < 0 {
		return fmt.Errorf("indexing.chunking.min_chunk_tokens must not be negative")
	}
	if ch.ProseOverlapRatio < 0 || ch.ProseOverlapRatio >= 1 {
		return fmt.Errorf("indexing.chunking.prose_overlap_ratio must be in [0, 1), got %v", ch.ProseOverlapRatio)
	}
	if ch.LineWindow.WindowLines < 0 || ch.LineWindow.OverlapLines < 0 {
		return fmt.Errorf("indexing.chunking.line_window values must not be negative")
	}
	fw := app.FileWatcher
	switch strings.ToLower(strings.TrimSpace(fw.Backend)) {
	case "", "auto", "inotify", "polling":
	default:
		return fmt.Errorf("file_watcher.backend %q must be auto, inotify or polling", fw.Backend)
	}
	if fw.DebounceSeconds < 0 || fw.PollIntervalSeconds < 0 {
		return fmt.Errorf("file_watcher timing values must not be negative")
	}
	return nil
}
