//go:build realworld && zvec

package harness

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// WithEnvFile writes key=value lines to .realworld/.env and restores on cleanup.
func WithEnvFile(t *testing.T, repo string, values map[string]string) {
	t.Helper()
	path := EnvPath(repo)
	var prev []byte
	if data, err := os.ReadFile(path); err == nil {
		prev = data
	}
	lines := make([]string, 0, len(values))
	for k, v := range values {
		lines = append(lines, fmt.Sprintf("%s=%s", k, v))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Cleanup(func() {
		if prev == nil {
			_ = os.Remove(path)
		} else {
			_ = os.WriteFile(path, prev, 0o600)
		}
	})
}

// EnvWithOverrides returns BaseEnv with additional KEY=value pairs applied.
func EnvWithOverrides(repo string, overrides map[string]string) []string {
	env := BaseEnv(repo)
	for k, v := range overrides {
		env = replaceEnvKey(env, k+"=", v)
	}
	return env
}
