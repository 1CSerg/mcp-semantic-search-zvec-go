//go:build !windows

package gui

import (
	"context"
	"fmt"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
)

// Run is implemented for Windows builds.
func Run(ctx context.Context, settings *config.Settings, svc service.Service) error {
	_ = ctx
	_ = settings
	_ = svc
	return fmt.Errorf("gui mode is currently available only on Windows")
}
