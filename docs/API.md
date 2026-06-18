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
      "snippet": "func AuthMiddleware(next http.Handler) http.Handler {\n\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n\t\t// ...\n\t})\n}",
      "symbol_name": "AuthMiddleware",
      "symbol_kind": "function",
      "parent_scope": "package middleware",
      "chunk_strategy": "ast"
    }
  ],
  "performance": { "total_ms": 42.1, "degraded": false }
}
```

**Symbol metadata** (populated after hybrid reindex; empty strings on legacy `line_window` indexes):

| Field | Description |
|-------|-------------|
| `symbol_name` | Name from AST boundary (`@name` capture), e.g. `AuthMiddleware`, `Server`; prose — heading title or empty for plain paragraphs |
| `symbol_kind` | Boundary kind: Go — `function`, `method`, `type`, `const`, `var`, `import`; Phase 1c — `class`, `interface`, `type_alias`, `enum`, `namespace`, `module_var`, …; **1C BSL** — `procedure`, `function`, `var`, `region`; **SDBL** (heuristic, not tree-sitter) — `query`, `query_package`; **prose** (Phase 1e) — `section`, `paragraph` |
| `parent_scope` | Scope chain, e.g. Go: `package auth > type Server > func Foo`; Python: `module sample > function handler > class Inner`; TS: `module sample > class UserService > method getUser`; **BSL:** `module Документ.РасходнаяНакладная.МодульОбъекта > region Проведение` (Cyrillic names preserved); **prose:** `section Intro > subsection Details` |
| `chunk_type` | Chunk category in zvec: `code` (default AST/line_window), **`markdown`** for prose (`.md`, `.markdown`, `.mdc`, `.txt`), or **`query`** for SDBL query text (from `.dcs` `<query>` blocks, embedded BSL strings, or heuristic SDBL split). Not returned in HTTP/MCP search JSON today; stored in the index for future filtering. |
| `chunk_strategy` | How the chunk was produced: `ast` (whole boundary), `partial` (AST or prose node split via `line_window`), **`prose`** (Markdown/plain prose chunker), `line_window` (legacy or fallback) |

**1C / SDBL:** BSL (`.bsl`, `.os`) uses **tree-sitter-bsl**. SDBL query chunks use **heuristic** boundary detection (`ВЫБРАТЬ`/`SELECT`, `;` splits) — not tree-sitter — even when emitted from BSL embedded strings or `.dcs` XML extraction (`languages.bsl.include_sdbl: true`).

`snippet` is always raw source from the file. When `context_prefix: true`, the embed model sees an extra header (`// file:` / `// scope:`), but that prefix is **not** stored in `snippet`.

During indexing: HTTP 200 with partial `results` from already indexed chunks, plus `indexing` progress object and optional `message` warning that results may be incomplete.

In shared daemon mode with **no** `API_TOKEN`, open-daemon redaction applies (see below): result `path` values stay relative to the workspace; absolute paths in `message` are sanitized.

---

### Status

#### `GET /v1/status`

Same JSON as MCP `index_status`. In shared daemon mode, pass `?workspace_id=` or header `X-Workspace-ID`.

The `indexing` object includes the lifecycle state plus progress details when available: `files_total`, `files_done`, `percent`, `remaining_seconds`, `chunks_indexed`, `current_file`, timestamps, warnings, and errors. `remaining_seconds` is an estimate based on file throughput and may be absent before enough progress is known.

Without `API_TOKEN` on an open daemon, `current_file`, `failed_files`, and `skipped_paths` are omitted; other path fields and diagnostics log paths are redacted (see [Open daemon redaction](#open-daemon-redaction)).

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

With `"force": true`, the server wipes the index and rebuilds from scratch. Required after changing `WORKSPACE_ROOT` / project path, `active_profile`, embedding dimensions, or chunking settings (`chunking_version`, `chunking_strategy`, `languages.*`, `context_prefix`, embed budget fields) when `index_meta.json` no longer matches (otherwise indexing fails with `index_owner_mismatch` or `identity_mismatch`).

---

### Ready (workspace-scoped)

#### `GET /ready`

In shared daemon mode, pass `?workspace_id=` or header `X-Workspace-ID`.

---

### Version

#### `GET /v1/version`

Installed version info (same payload as MCP `check_update`). Production builds poll [GitHub Releases](https://github.com/1CSerg/mcp-semantic-search-zvec-go/releases) (success cache 1 h, error cache 1 min). Set `CHECK_UPDATE_DISABLE=true` to skip polling. Configure repo via `GITHUB_REPO` (see [CONFIG.md](CONFIG.md#environment-variables)). Stub build (`!zvec`) returns a placeholder without calling GitHub.

Example (update available):

```json
{
  "installed_version": "0.1.7",
  "latest_version": "0.1.8",
  "update_available": true,
  "github_repo": "1CSerg/mcp-semantic-search-zvec-go",
  "release_url": "https://github.com/1CSerg/mcp-semantic-search-zvec-go/releases/tag/v0.1.8"
}
```

---

### Workspaces (shared daemon)

#### `GET /v1/workspaces`

List registered workspaces in shared daemon mode (`--daemon`). Returns `501` in per-project mode.

By default the response includes only workspace `id` and whether the handle is currently `open` — no filesystem paths.

Query parameter `include_paths=1` (or `true` / `yes`) adds absolute `root`, `index_dir`, and `config_path` from `daemon.yaml`. `include_paths` always requires a valid `Authorization: Bearer` header matching `API_TOKEN` (returns `401` when the token is unset or the header is missing/invalid).

Default response:

```json
{
  "workspaces": [
    {
      "id": "my-app",
      "open": true
    }
  ]
}
```

With `?include_paths=1` and valid Bearer token:

```json
{
  "workspaces": [
    {
      "id": "my-app",
      "open": true,
      "root": "/path/to/my-app",
      "index_dir": "/path/to/my-app/.mcp-semantic-search-zvec-go/data/index",
      "config_path": "/path/to/my-app/.mcp-semantic-search-zvec-go/config.yaml"
    }
  ]
}
```

---

### Open daemon redaction

When the shared daemon runs **without** `API_TOKEN` (open HTTP API), workspace-scoped responses from `GET /v1/status`, `POST /v1/search`, and `POST /v1/reindex` are redacted before send:

| Removed / redacted | Kept |
|--------------------|------|
| Top-level `workspace_root`, `index_dir`, `config_path`, `zvec_collection_path`, `embedding_model_path` | Counts, versions, `indexing.state`, relative search result `path` |
| `diagnostics.log_dir`, `diagnostics.log_file` | `diagnostics.hint` and other non-path flags |
| `indexing` / `progress`: `current_file`, `failed_files`, `skipped_paths` | Numeric progress (`files_done`, `percent`, …) |
| Absolute paths inside `message`, `error`, `zvec_error`, `identity_mismatch_reason`, `indexing.message`, `indexing.error`, `scan_warnings` | Text with paths replaced by `<redacted>` |

Set `API_TOKEN` in the daemon environment (`.env` or process env) and pass `Authorization: Bearer <token>` on HTTP requests (and configure the proxy accordingly) for full paths and file-level indexing diagnostics.

---

## CLI flags

Windows no flags → desktop GUI. Linux/macOS no flags → `--stdio` (per-project MCP). `--version` / `-version` prints version and exits.

| Flag | Mode | Description |
|------|------|-------------|
| `--gui` | Desktop GUI | Windows desktop UI for status, reindex, search, and result viewing |
| `--stdio` | Per-project | MCP server over stdin/stdout |
| `--http` | Per-project | HTTP REST API |
| `--http-addr` | Both | Listen address (default `127.0.0.1:8080` per-project, `:8080` daemon, or `server.http_addr`) |
| `--config` | Per-project | Override `CONFIG_PATH` |
| `--daemon` | Shared daemon | Multi-workspace HTTP daemon (implies `--http`) |
| `--daemon-config` | Shared daemon | Path to `daemon.yaml` (or `WORKSPACES_CONFIG`) |
| `--stdio-proxy` | Proxy | MCP stdio → HTTP proxy (requires `--workspace-id`) |
| `--workspace-id` | Proxy | Workspace ID for `--stdio-proxy` |
| `--daemon-url` | Proxy | Daemon base URL (default `http://127.0.0.1:8080`) |
| `--stop-stdio-for-workspace` | Maintenance | Stop stale `--stdio` MCP instances for workspace and exit |
| `--index-dir` | Maintenance | Index directory for lock reclaim (optional; with `--stop-stdio-for-workspace`) |

Per-project MCP: `--stdio` and/or `--http` with `WORKSPACE_ROOT` env. Shared daemon: `--daemon --daemon-config …`. Cursor multi-repo: `--stdio-proxy --workspace-id=<id> --daemon-url=…`.

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

Identity fields (when `index_meta.json` exists): `active_profile`, `index_embedding_profile`, `index_embedding_dimensions`, `index_collection_name`, **`chunking_strategy`**, **`chunking_version`**, **`index_chunking_version`**, **`index_chunking_strategy`**. Compare config vs on-disk values to detect stale indexes after chunking upgrades. When metadata does not match the current profile/dimensions/chunking settings: `identity_mismatch: true` and `identity_mismatch_reason` (e.g. profile mismatch, `chunking_version mismatch`). `message` may summarize required action (`reindex` with `force: true`).

When indexing finishes with per-file zvec/read errors, `indexing.state` is `idle` (not `error`) and `indexing.files_failed` shows how many files were skipped. Skipped paths (up to 20) appear in `indexing.failed_files`. Details are also in `diagnostics.log_file` (`index file skipped` in server.log). Via shared daemon proxy without `API_TOKEN`, `failed_files`, `current_file`, and path fields are redacted — see [Open daemon redaction](#open-daemon-redaction).

`diagnostics` includes `log_dir`, `log_file`, and optional hints: `synced_cloud_drive_suspected` (Google Drive/YandexDisk paths), `unicode_index_path_suspected` only when zvec fails to open a non-ASCII `INDEX_DIR` on Windows. With v0.1.5+, Cyrillic paths are supported when `zvec_open_ok` is true.

Fatal failures (embed provider down, `index_owner_mismatch`, stall) still set `indexing.state` to `error`.

### `reindex`

| Argument | Type | Default | Description |
|----------|------|---------|-------------|
| `force` | bool | false | Full reindex. With `force: true`, also resets the index when `workspace_root`, `active_profile`, embedding `dimensions`, or **chunking identity** changed (`chunking_version`, `chunking_strategy`, language toggles, `context_prefix`, `max_input_tokens` / `embed_budget_ratio`). Incremental reindex without `force` returns `index_owner_mismatch` if metadata does not match. |

### `check_update`

No arguments. Returns `installed_version`, `latest_version`, `update_available`, `github_repo`, optional `release_url` and `message`.

Production builds poll GitHub Releases. Successful responses are cached **1 h**; failed polls cache **1 min** before retry. `CHECK_UPDATE_DISABLE=true` skips polling and sets `message: "update check disabled"`. Stub build (`!zvec`) returns a placeholder without calling GitHub. HTTP equivalent: `GET /v1/version`.

---

## Error semantics

| Condition | HTTP | MCP |
|-----------|------|-----|
| Invalid JSON | 400 | tool error |
| Invalid search `limit` (negative) | 400 | tool error |
| Indexing in progress (search) | 200 | JSON with partial `results` + `indexing` + warning `message` |
| Missing `workspace_id` (daemon) | 400 | tool error (proxy) |
| Unknown `workspace_id` (daemon) | 404 | tool error (proxy) |
| `include_paths` without valid Bearer | 401 | — |
| Registry shutting down (`registry is closing`) | 503 | tool error (proxy) |
| `GET /v1/workspaces` in per-project mode | 501 | — |
| Index owner mismatch | 200* | JSON with message + empty results |
| Internal error | 500 | tool error |

\* Search returns structured JSON rather than HTTP error for agent-friendly handling.
