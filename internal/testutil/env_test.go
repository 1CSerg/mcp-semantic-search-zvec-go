package testutil

import (
	"slices"
	"strings"
	"testing"
)

func TestHelperProcessEnvStripsGOCOVERDIR(t *testing.T) {
	t.Setenv("GOCOVERDIR", t.TempDir())
	t.Setenv("HELPER_PROCESS_ENV_TEST", "keep")

	env := HelperProcessEnv("EXTRA=1")

	for _, e := range env {
		if strings.HasPrefix(e, "GOCOVERDIR=") {
			t.Fatalf("GOCOVERDIR must be stripped, got %q", e)
		}
	}
	if !slices.Contains(env, "HELPER_PROCESS_ENV_TEST=keep") {
		t.Fatal("expected existing env var to be preserved")
	}
	if !slices.Contains(env, "EXTRA=1") {
		t.Fatal("expected extra env var to be appended")
	}
}

func TestHelperProcessEnvNoExtra(t *testing.T) {
	t.Setenv("HELPER_PROCESS_ENV_ONLY", "yes")

	env := HelperProcessEnv()
	for _, e := range env {
		if strings.HasPrefix(e, "GOCOVERDIR=") {
			t.Fatalf("GOCOVERDIR must be stripped, got %q", e)
		}
	}
	if !slices.Contains(env, "HELPER_PROCESS_ENV_ONLY=yes") {
		t.Fatal("expected env var to be preserved")
	}
}
