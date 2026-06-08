package scan

import (
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

// Discover returns relative file paths to index.
func Discover(opts Options) ([]string, error) {
	if opts.Root == "" {
		return nil, os.ErrInvalid
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	if files, err := gitFiles(root); err == nil && len(files) > 0 {
		return filterFiles(files, opts), nil
	}
	return walkFiles(root, opts)
}

func gitFiles(root string) ([]string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\\", "/"))
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func walkFiles(root string, opts Options) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
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
			return nil
		}
		rel = filepath.ToSlash(rel)
		if matchesExtension(rel, opts.Extensions) {
			files = append(files, rel)
		}
		return nil
	})
	return files, err
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
