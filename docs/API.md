# API Reference

## HTTP REST (v1)

Base URL: `http://127.0.0.1:8080` (configurable via `server.http_addr` / `HTTP_ADDR`).

Optional auth: `Authorization: Bearer <API_TOKEN>` when `API_TOKEN` env is set.

### Health

#### `GET /health`

Liveness probe.

```json
{
  "status": "ok",
  "service": "mcp-semantic-search-zvec-go",
  "version": "0.1.3"
}
```

#### `GET /ready`

Readiness — index built, embeddings reachable, indexing idle.

Success (`200`):

```json
{ "status": "ready" }
```

Not ready (`503`):

```json
{
  "status": "not_ready",
  "error": "indexing in progress"
}
```

Common `error` values: `indexing in progress`, `embedding provider not configured`, `index not built yet`, `embeddings unreachable: ...`, `index_owner_mismatch: ...`.

---

### Search

#### `POST /v1/search`

Request:

```json
{
  "query": "authentication middleware",
  "limit": 10,
  "path_glob": "**/*.go"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | yes | Natural language query |
| `limit` | int | no | Max results (default 10) |
| `path_glob` | string | no | Filter result paths |
| `top_k` | int | no | Deprecated alias for `limit` |
| `workspace_id` | string | no | Required in shared daemon mode (`--daemon`) |

Response (success):

```json
{
  "query": "authentication middleware",
  "results": [
    {
      "start_line": 12,
      "end_line": 45,
      "path": "internal/auth/middleware.go",
      "score": 0.87,
      "snippet": "..."
    }
  ],
  "performance": { "total_ms": 42.1, "degraded": false }
}
```

During indexing: HTTP 200 with partial `results` from already indexed chunks, plus `indexing` progress object and optional `message` warning that results may be incomplete.

---

### Status

#### `GET /v1/status`

Same JSON as MCP `index_status`. In shared daemon mode, pass `?workspace_id=` or header `X-Workspace-ID`.

---

### Reindex

#### `POST /v1/reindex`

Request:

```json
{
  "force": false,
  "workspace_id": "my-app"
}
```

In shared daemon mode, `workspace_id` is required (JSON body, query, or `X-Workspace-ID` header).

Starts background indexing; returns initial progress.

With `"force": true`, the server wipes the index and rebuilds from scratch. This is required after changing `WORKSPACE_ROOT` / project path, `active_profile`, or embedding dimensions when `index_meta.json` no longer matches (otherwise indexing fails with `index_owner_mismatch`).

---

### Ready (workspace-scoped)

#### `GET /ready`

In shared daemon mode, pass `?workspace_id=` or header `X-Workspace-ID`.

---

### Version

#### `GET /v1/version`

Installed version info. **Stub:** does not query GitHub Releases; `latest_version` always equals installed and `update_available` is always `false`. Compare with [GitHub Releases](https://github.com/1CSerg/mcp-semantic-search-zvec-go/releases) manually for updates.

Example:

```json
{
  "installed_version": "0.1.3",
  "latest_version": "0.1.3",
  "update_available": false,
  "github_repo": "1CSerg/mcp-semantic-search-zvec-go"
}
```

---

### Workspaces (shared daemon)

#### `GET /v1/workspaces`

List registered workspaces in shared daemon mode (`--daemon`). Returns `501` in per-project mode.

Response:

```json
{
  "workspaces": [
    {
      "id": "my-app",
      "root": "/path/to/my-app",
      "index_dir": "/path/to/my-app/.mcp-semantic-search-zvec-go/data/index",
      "config_path": "/path/to/my-app/.mcp-semantic-search-zvec-go/config.yaml",
      "open": true
    }
  ]
}
```

---

## CLI flags

No flags → `--stdio` (per-project MCP). `--version` / `-version` prints version and exits.

| Flag | Mode | Description |
|------|------|-------------|
| `--stdio` | Per-project | MCP server over stdin/stdout |
| `--http` | Per-project | HTTP REST API |
| `--http-addr` | Both | Listen address (default `:8080` or `server.http_addr`) |
| `--config` | Per-project | Override `CONFIG_PATH` |
| `--daemon` | Shared daemon | Multi-workspace HTTP daemon (implies `--http`) |
| `--daemon-config` | Shared daemon | Path to `daemon.yaml` (or `WORKSPACES_CONFIG`) |
| `--stdio-proxy` | Proxy | MCP stdio → HTTP proxy (requires `--workspace-id`) |
| `--workspace-id` | Proxy | Workspace ID for `--stdio-proxy` |
| `--daemon-url` | Proxy | Daemon base URL (default `http://127.0.0.1:8080`) |
| `--stop-stdio-for-workspace` | Maintenance | Stop stale `--stdio` MCP instances for workspace and exit |
| `--index-dir` | Maintenance | Index directory for lock reclaim (optional; with `--stop-stdio-for-workspace`) |

Per-project (default): `--stdio` and/or `--http` with `WORKSPACE_ROOT` env. Shared daemon: `--daemon --daemon-config …`. Cursor multi-repo: `--stdio-proxy --workspace-id=<id> --daemon-url=…`.

See also [DEVELOPMENT.md](DEVELOPMENT.md#run-modes).

---

## MCP tools

Transport: stdio JSON-RPC via [MCP go-sdk](https://github.com/modelcontextprotocol/go-sdk).

| Name | Value | Where |
|------|-------|-------|
| Cursor `mcp.json` key | `semantic-search-zvec-go` | `.cursor/mcp.json` |
| MCP `implementation.name` | `mcp-semantic-search-zvec-go` | MCP handshake |

All tools return **JSON text** in tool result content.

### `semantic_search`

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `query` | string | yes | NL query |
| `limit` | int | no | Result count (default 10) |
| `path_glob` | string | no | Path filter |

### `index_status`

No arguments. Returns paths, counts, `indexing`, `file_watcher`, `search_performance`, `diagnostics`.

When indexing finishes with per-file zvec/read errors, `indexing.state` is `idle` (not `error`) and `indexing.files_failed` shows how many files were skipped. Details are written to `data/logs/server.log` (`index file skipped`).

Fatal failures (embed provider down, `index_owner_mismatch`, stall) still set `indexing.state` to `error`.

### `reindex`

| Argument | Type | Default | Description |
|----------|------|---------|-------------|
| `force` | bool | false | Full reindex. With `force: true`, also resets the index when `workspace_root`, `active_profile`, or embedding `dimensions` changed (e.g. after moving the project). Incremental reindex without `force` returns `index_owner_mismatch` if metadata does not match. |

### `check_update`

No arguments. Returns `installed_version`, `latest_version`, `update_available`, `github_repo`.

**Stub:** does not call GitHub Releases API. `latest_version` always equals installed; `update_available` is always `false`. For real update checks, compare `installed_version` with [GitHub Releases](https://github.com/1CSerg/mcp-semantic-search-zvec-go/releases).

---

## Error semantics

| Condition | HTTP | MCP |
|-----------|------|-----|
| Invalid JSON | 400 | tool error |
| Indexing in progress (search) | 200 | JSON with partial `results` + `indexing` + warning `message` |
| Missing `workspace_id` (daemon) | 400 | tool error (proxy) |
| Unknown `workspace_id` (daemon) | 404 | tool error (proxy) |
| Index owner mismatch | 200* | JSON with message + empty results |
| Internal error | 500 | tool error |

\* Search returns structured JSON rather than HTTP error for agent-friendly handling.
