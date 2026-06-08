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
  "version": "1.0.0"
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
      "path": "internal/auth/middleware.go",
      "start_line": 12,
      "end_line": 45,
      "score": 0.87,
      "snippet": "..."
    }
  ],
  "timing": { "total_ms": 42.1 },
  "performance": { "degraded": false }
}
```

During indexing: HTTP 409 with `indexing` progress object.

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

---

### Ready (workspace-scoped)

#### `GET /ready`

In shared daemon mode, pass `?workspace_id=` or header `X-Workspace-ID`.

---

### Version

#### `GET /v1/version`

Installed vs latest GitHub release.

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

## Shared daemon CLI

| Flag | Description |
|------|-------------|
| `--daemon` | Run shared multi-workspace HTTP daemon |
| `--daemon-config` | Path to `daemon.yaml` (or `WORKSPACES_CONFIG`) |
| `--stdio-proxy` | MCP stdio → HTTP proxy (use with `--workspace-id`) |
| `--workspace-id` | Workspace ID for `--stdio-proxy` |
| `--daemon-url` | Daemon base URL for proxy (default `http://127.0.0.1:8080`) |

Per-project mode (default): `--stdio` and/or `--http` with `WORKSPACE_ROOT` env (unchanged).

---

## MCP tools

Transport: stdio JSON-RPC via [MCP go-sdk](https://github.com/modelcontextprotocol/go-sdk).

Server name: `semantic-search-zvec-go` (config key in `.cursor/mcp.json`).

All tools return **JSON text** in tool result content.

### `semantic_search`

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `query` | string | yes | NL query |
| `limit` | int | no | Result count (default 10) |
| `path_glob` | string | no | Path filter |
| `top_k` | int | no | Deprecated alias |

### `index_status`

No arguments. Returns paths, counts, `indexing`, `file_watcher`, `search_performance`, `diagnostics`.

### `reindex`

| Argument | Type | Default |
|----------|------|---------|
| `force` | bool | false |

### `check_update`

No arguments. Returns `installed_version`, `latest_version`, `update_available`.

---

## Error semantics

| Condition | HTTP | MCP |
|-----------|------|-----|
| Invalid JSON | 400 | tool error |
| Indexing in progress | 409 | JSON with empty `results` + `indexing` |
| Missing `workspace_id` (daemon) | 400 | tool error (proxy) |
| Unknown `workspace_id` (daemon) | 404 | tool error (proxy) |
| Index owner mismatch | 200* | JSON with message + empty results |
| Internal error | 500 | tool error |

\* Search returns structured JSON rather than HTTP error for agent-friendly handling.
