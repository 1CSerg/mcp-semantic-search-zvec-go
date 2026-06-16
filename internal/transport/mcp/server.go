package mcptransport

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

const instructions = `MCP-сервер semantic-search-zvec-go: инструменты вызываются только через MCP.

- Исследование кодовой базы → semantic_search, не полный обход репозитория.
- Статус индекса → index_status; переиндексация → reindex.
- При indexing.running результаты semantic_search могут быть неполными; смотрите поле indexing в ответе.
- semantic_search: только query и опционально limit (default 10), path_glob; не передавайте top_k.
- HTTP REST API доступен при запуске с --http (см. docs/API.md).`

// semanticSearchInput is the MCP tool schema; HTTP keeps top_k on service.SearchRequest.
type semanticSearchInput struct {
	Query    string  `json:"query" jsonschema:"Natural language search query."`
	Limit    int     `json:"limit,omitempty" jsonschema:"Maximum number of results (default 10). Use only this parameter, not top_k."`
	PathGlob *string `json:"path_glob,omitempty" jsonschema:"Optional glob to filter result file paths."`
}

func (in semanticSearchInput) toSearchRequest() service.SearchRequest {
	return service.SearchRequest{
		Query:    in.Query,
		Limit:    in.Limit,
		PathGlob: in.PathGlob,
	}
}

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
		Description: "Semantic search over indexed code. Call first when exploring a project. Use limit for result count (default 10); do not pass top_k.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input semanticSearchInput) (*mcp.CallToolResult, any, error) {
		_ = req
		raw, err := svc.SemanticSearch(ctx, input.toSearchRequest())
		if err != nil {
			return toolError(err)
		}
		return textResult(string(raw)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "index_status",
		Description: "Index status: paths, counts, indexing progress, diagnostics.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		_ = req
		raw, err := svc.GetIndexStatus(ctx)
		if err != nil {
			return toolError(err)
		}
		return textResult(string(raw)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "reindex",
		Description: "Full (force=true) or incremental reindex in background.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input service.ReindexRequest) (*mcp.CallToolResult, any, error) {
		_ = req
		raw, err := svc.Reindex(ctx, input)
		if err != nil {
			return toolError(err)
		}
		return textResult(string(raw)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_update",
		Description: "Compare installed version with latest GitHub release.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		_ = req
		raw, err := svc.CheckUpdate(ctx)
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
	// stdio MCP is a local, single-user transport; the detailed message is
	// intentionally surfaced to the agent (e.g. index_owner_mismatch hints).
	slog.Warn("mcp tool error", "err", err)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("error: %v", err)},
		},
		IsError: true,
	}, nil, nil
}
