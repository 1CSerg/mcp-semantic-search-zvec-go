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
  "version": "0.1.0"
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
| `workspace_id` | string | no | Required in shared daemon mode (Phase 5) |

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

Same JSON as MCP `index_status`. Optional query `?workspace_id=` (Phase 5).

---

### Reindex

#### `POST /v1/reindex`

Request:

```json
{ "force": false }
```

Starts background indexing; returns initial progress.

---

### Version

#### `GET /v1/version`

Installed vs latest GitHub release.

---

### Workspaces (Phase 5)

#### `GET /v1/workspaces`

List registered workspaces in shared daemon mode. Returns `501` until Phase 5.

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
| Index owner mismatch | 200* | JSON with message + empty results |
| Internal error | 500 | tool error |

\* Search returns structured JSON rather than HTTP error for agent-friendly handling.
