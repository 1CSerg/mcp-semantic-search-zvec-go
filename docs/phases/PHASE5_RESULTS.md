# Phase 5 Results

## Scope delivered

- `internal/config` — `LoadOptions`, `LoadWithOptions`, `ParseDotEnv`, per-workspace secrets without global env leakage
- `internal/daemon` — `daemon.yaml` parsing, `WorkspaceRegistry` with lazy open, LRU eviction, graceful close
- `internal/service/proxy.go` — HTTP-backed MCP stdio proxy (`HTTPProxy`)
- `internal/transport/http` — multi-tenant routing via `X-Workspace-ID` / query / JSON `workspace_id`; `GET /v1/workspaces`
- `cmd/mcp-semantic-search-zvec-go` — `--daemon`, `--daemon-config`, `--stdio-proxy`, `--workspace-id`, `--daemon-url`
- Templates: `templates/daemon.yaml`, `templates/cursor-mcp-proxy*.fragment.json`
- `scripts/smoke/run-phase5.*` — gate smoke (3 workspaces, isolation, concurrent status, MCP proxy test)
- Version bump to **v1.0.0** in `internal/version/version.go`

## Gate

| Check | Command | Result |
|-------|---------|--------|
| Unit tests | `go test ./...` | Pass |
| Coverage ≥88% `./internal/...`, ≥50% per package | `make test-cover-check` / `scripts/dev/check-coverage.ps1` | Pass (88.0%) |
| Phase 5 smoke (Windows) | `.\scripts\smoke\run-phase5.ps1` | Pass (2026-06-08) |
| MCP proxy round-trip | `go test ./internal/transport/mcp/ -run TestMCPOverHTTPProxy` | Pass (included in smoke) |

Smoke validates:

1. One `--daemon` process serves three registered workspaces from `daemon.yaml`
2. `GET /v1/workspaces` lists all workspaces
3. Per-workspace `reindex` + `semantic_search` via `workspace_id` / `X-Workspace-ID`
4. No cross-workspace snippet leakage on mismatched queries
5. Concurrent `/v1/status` polling across workspaces (LRU with `max_open_workspaces: 2`)
6. MCP tools over `--stdio-proxy` HTTP adapter (`TestMCPOverHTTPProxy`)

## Shared daemon usage

```bash
# Start daemon
bin/mcp-semantic-search-zvec-go --daemon --daemon-config /path/to/daemon.yaml --http-addr :8080

# Cursor MCP proxy (per project)
bin/mcp-semantic-search-zvec-go --stdio-proxy --workspace-id=my-app --daemon-url=http://127.0.0.1:8080
```

Per-project mode (`--stdio` with `WORKSPACE_ROOT` env) is unchanged.

## Known limitations

- Daemon HTTP logging uses a minimal global settings object (no per-workspace log dir in daemon process).
- `file_watcher.run_as_daemon` remains unused; watchers run inside the daemon per open workspace.
- LRU eviction closes zvec handles; next request re-opens the workspace (small latency).
- Release tag `v1.0.0` should be created separately after merge; workflow validates version match in `internal/version/version.go`.
