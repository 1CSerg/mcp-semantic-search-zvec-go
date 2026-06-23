//go:build !windows

package gui

import (
	"context"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestRunStubReturnsError(t *testing.T) {
	err := Run(context.Background(), &config.Settings{}, nil)
	if err == nil {
		t.Fatal("expected error on non-Windows stub")
	}
	if err.Error() != "gui mode is currently available only on Windows" {
		t.Fatalf("unexpected error: %v", err)
	}
}
