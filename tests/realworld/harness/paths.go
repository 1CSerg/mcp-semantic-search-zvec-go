//go:build realworld && zvec

package harness

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	RealworldDirName = ".realworld"
	CorpusRel        = "tests/realworld/corpus"
	ConfigRel        = "tests/realworld/config"
)

// RepoRoot returns the repository root (directory containing go.mod).
func RepoRoot(t *testing.T) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("REALWORLD_REPO_ROOT")); v != "" {
		return v
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found; run from repo root or set REALWORLD_REPO_ROOT")
		}
		dir = parent
	}
}

func RealworldRoot(repo string) string {
	return filepath.Join(repo, RealworldDirName)
}

func CorpusDir(repo string) string {
	return filepath.Join(repo, filepath.FromSlash(CorpusRel))
}

func BinDir(repo string) string {
	return filepath.Join(RealworldRoot(repo), "bin")
}

func BinPath(repo string) string {
	name := "mcp-semantic-search-zvec-go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(BinDir(repo), name)
}

func ConfigPath(repo string) string {
	return filepath.Join(RealworldRoot(repo), "config.yaml")
}

func IndexDir(repo string) string {
	return filepath.Join(RealworldRoot(repo), "data", "index")
}

func EnvPath(repo string) string {
	return filepath.Join(RealworldRoot(repo), ".env")
}

func ConfigTemplate(repo, profile string) string {
	switch profile {
	case "lmstudio":
		return filepath.Join(repo, filepath.FromSlash(ConfigRel), "lmstudio.yaml")
	case "mock-fail":
		return filepath.Join(repo, filepath.FromSlash(ConfigRel), "mock-fail.yaml")
	case "mock-dim-mismatch":
		return filepath.Join(repo, filepath.FromSlash(ConfigRel), "mock-dim-mismatch.yaml")
	case "mock-retry":
		return filepath.Join(repo, filepath.FromSlash(ConfigRel), "mock-retry.yaml")
	case "mock-api-key":
		return filepath.Join(repo, filepath.FromSlash(ConfigRel), "mock-api-key.yaml")
	case "daemon-workspace":
		return filepath.Join(repo, filepath.FromSlash(ConfigRel), "daemon-workspace.yaml")
	default:
		return filepath.Join(repo, filepath.FromSlash(ConfigRel), "onnx.yaml")
	}
}

func InstallSmokeTarget(repo string) string {
	return filepath.Join(RealworldRoot(repo), "targets", "install-smoke")
}

// RequireHarness skips the test when setup-harness has not been run.
func RequireHarness(t *testing.T) string {
	t.Helper()
	repo := RepoRoot(t)
	bin := BinPath(repo)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("realworld harness not ready (missing %s); run scripts/realworld/setup-harness", bin)
	}
	cfg := ConfigPath(repo)
	if _, err := os.Stat(cfg); err != nil {
		t.Skipf("realworld harness not ready (missing %s); run scripts/realworld/setup-harness", cfg)
	}
	return repo
}
