//go:build windows && !cgo

package gui

import (
	"context"
	"fmt"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
)

// Run is unavailable when CGO is disabled (Fyne requires CGO on Windows).
func Run(ctx context.Context, settings *config.Settings, svc service.Service) error {
	_ = ctx
	_ = settings
	_ = svc
	return fmt.Errorf("gui mode requires CGO on Windows (build with CGO_ENABLED=1)")
}
