package scan

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Options configures workspace file discovery.
type Options struct {
	Root       string
	Extensions []string
	SkipDirs   []string
}

// Result describes discovered files and scan diagnostics.
type Result struct {
	Files        []string
	Method       string
	SkippedPaths []string
	Warnings     []string
}

// Discover returns relative file paths to index with scan metadata.
func Discover(opts Options) (Result, error) {
	if opts.Root == "" {
		return Result{}, os.ErrInvalid
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return Result{}, err
	}
	gitRes, ok := discoverGit(root, opts)
	if ok {
		return gitRes, nil
	}
	walkRes, err := discoverWalk(root, opts)
	if err != nil {
		return walkRes, err
	}
	// Preserve the reason git discovery was skipped (git_not_found /
	// git_unavailable / empty_repository) so index_status can surface it.
	if len(gitRes.Warnings) > 0 {
		walkRes.Warnings = append(gitRes.Warnings, walkRes.Warnings...)
	}
	return walkRes, nil
}

func discoverGit(root string, opts Options) (Result, bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return Result{Warnings: []string{"git_not_found: falling back to directory walk"}}, false
	}
	gitDir := filepath.Join(root, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return Result{}, false
	}
	files, err := gitFiles(root)
	if err != nil {
		return Result{Warnings: []string{"git_unavailable: " + err.Error()}}, false
	}
	if len(files) == 0 {
		return Result{Warnings: []string{"empty_repository: git ls-files returned no paths"}}, false
	}
	files, skipped := excludeMissingGitFiles(root, files)
	return Result{
		Files:        filterFiles(files, opts),
		SkippedPaths: skipped,
		Method:       "git",
	}, true
}

func gitFiles(root string) ([]string, error) {
	return gitCommandLines(root, "ls-files", "--cached", "--others", "--exclude-standard")
}

func gitDeletedFiles(root string) ([]string, error) {
	return gitCommandLines(root, "ls-files", "--deleted")
}

func gitCommandLines(root string, args ...string) ([]string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseGitLsFilesOutput(out), nil
}

func parseGitLsFilesOutput(out []byte) []string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\\", "/"))
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// excludeMissingGitFiles drops tracked paths deleted from the working tree but
// still listed by git ls-files --cached (uncommitted git rm).
func excludeMissingGitFiles(root string, files []string) ([]string, []string) {
	deleted, err := gitDeletedFiles(root)
	if err != nil {
		slog.Debug("git ls-files --deleted failed; falling back to stat", "err", err)
		return excludeMissingByStat(root, files)
	}
	if len(deleted) == 0 {
		return files, nil
	}
	delSet := make(map[string]struct{}, len(deleted))
	for _, rel := range deleted {
		delSet[rel] = struct{}{}
	}
	var kept, skipped []string
	for _, rel := range files {
		if _, missing := delSet[rel]; missing {
			skipped = append(skipped, rel)
			continue
		}
		kept = append(kept, rel)
	}
	return kept, skipped
}

func excludeMissingByStat(root string, files []string) ([]string, []string) {
	var kept, skipped []string
	for _, rel := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(abs); err != nil {
			if os.IsNotExist(err) {
				skipped = append(skipped, rel)
				continue
			}
		}
		kept = append(kept, rel)
	}
	return kept, skipped
}

func discoverWalk(root string, opts Options) (Result, error) {
	var result Result
	result.Method = "walk"
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.SkippedPaths = append(result.SkippedPaths, relForSkip(root, path))
			slog.Debug("scan skipped path", "path", path, "err", walkErr)
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipDir(filepath.Base(path), opts.SkipDirs) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			result.SkippedPaths = append(result.SkippedPaths, relForSkip(root, path))
			slog.Debug("scan skipped path", "path", path, "err", err)
			return nil
		}
		rel = filepath.ToSlash(rel)
		if matchesExtension(rel, opts.Extensions) {
			result.Files = append(result.Files, rel)
		}
		return nil
	})
	return result, err
}

// relForSkip returns path relative to root (slash-separated) for diagnostics,
// falling back to the original path if it cannot be relativized.
func relForSkip(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func filterFiles(files []string, opts Options) []string {
	var out []string
	for _, rel := range files {
		if shouldSkipPath(rel, opts.SkipDirs) {
			continue
		}
		if matchesExtension(rel, opts.Extensions) {
			out = append(out, rel)
		}
	}
	return out
}

func shouldSkipPath(rel string, skipDirs []string) bool {
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if shouldSkipDir(p, skipDirs) {
			return true
		}
	}
	return false
}

func shouldSkipDir(name string, skipDirs []string) bool {
	for _, s := range skipDirs {
		if name == s {
			return true
		}
	}
	return false
}

// MatchesWatchPath reports whether a relative path should trigger watcher reindex.
func MatchesWatchPath(rel string, extensions, skipDirs []string) bool {
	if shouldSkipPath(rel, skipDirs) {
		return false
	}
	return matchesExtension(rel, extensions)
}

func matchesExtension(rel string, extensions []string) bool {
	if len(extensions) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(rel))
	for _, want := range extensions {
		w := strings.ToLower(want)
		if !strings.HasPrefix(w, ".") {
			w = "." + w
		}
		if ext == w {
			return true
		}
	}
	return false
}
