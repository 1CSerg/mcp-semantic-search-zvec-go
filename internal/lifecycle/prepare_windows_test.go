//go:build windows

package lifecycle

import (
	"os"
	"testing"
)

func TestProcessCommandLineCurrentProcess(t *testing.T) {
	cmdline, err := processCommandLine(uint32(os.Getpid()))
	if err != nil {
		t.Fatalf("processCommandLine: %v", err)
	}
	if cmdline == "" {
		t.Fatal("expected non-empty command line")
	}
}
