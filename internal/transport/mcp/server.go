package mcptransport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

const instructions = `MCP-сервер semantic-search-zvec-go: инструменты вызываются только через MCP.

- Исследование кодовой базы → semantic_search, не полный обход репозитория.
- Статус индекса → index_status; переиндексация → reindex.
- При indexing.running — poll index_status до idle, затем повторите semantic_search.
- HTTP REST API доступен при запуске с --http (см. docs/API.md).`

// Run starts the MCP server over stdio until the client disconnects.
func Run(ctx context.Context, svc service.Service) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    version.Name,
		Version: version.Version,
	}, &mcp.ServerOptions{
		Instructions: instructions,
	})

	registerTools(server, svc)

	slog.Info("mcp stdio server starting", "name", version.Name, "version", version.Version)
	return server.Run(ctx, &mcp.StdioTransport{})
}

func registerTools(server *mcp.Server, svc service.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "semantic_search",
		Description: "Semantic search over indexed code. Call first when exploring a project.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input service.SearchRequest) (*mcp.CallToolResult, any, error) {
		_ = ctx
		_ = req
		raw, err := svc.SemanticSearch(input)
		if err != nil && !errors.Is(err, service.ErrIndexingInProgress) {
			return toolError(err)
		}
		return textResult(string(raw)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "index_status",
		Description: "Index status: paths, counts, indexing progress, diagnostics.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		_ = ctx
		_ = req
		raw, err := svc.GetIndexStatus()
		if err != nil {
			return toolError(err)
		}
		return textResult(string(raw)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "reindex",
		Description: "Full (force=true) or incremental reindex in background.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input service.ReindexRequest) (*mcp.CallToolResult, any, error) {
		_ = ctx
		_ = req
		raw, err := svc.Reindex(input)
		if err != nil {
			return toolError(err)
		}
		return textResult(string(raw)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_update",
		Description: "Compare installed version with latest GitHub release.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		_ = ctx
		_ = req
		raw, err := svc.CheckUpdate()
		if err != nil {
			return toolError(err)
		}
		return textResult(string(raw)), nil, nil
	})
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func toolError(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("error: %v", err)},
		},
		IsError: true,
	}, nil, nil
}
