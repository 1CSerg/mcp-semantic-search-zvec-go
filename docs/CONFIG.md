# Configuration

Configuration file: `.mcp-semantic-search-zvec-go/config.yaml` in the target project (or path from `CONFIG_PATH`).

Reference copy in repo root: [config.yaml](../config.yaml).

**Settings** live in `config.yaml`. **Secrets** (API keys, tokens) live in `.mcp-semantic-search-zvec-go/.env` (created on install from [templates/env.example](../templates/env.example)).

## Secrets (.env)

| Item | Description |
|------|-------------|
| Path | `.mcp-semantic-search-zvec-go/.env` next to `config.yaml` |
| Template | [templates/env.example](../templates/env.example) |
| Override path | `ENV_PATH` env var |
| Load order | `ENV_PATH` → `dirname(CONFIG_PATH)/.env` → `WORKSPACE_ROOT/.mcp-semantic-search-zvec-go/.env` |
| Priority | Variables already set (MCP `env`, shell) are **not** overwritten by `.env` |
| Custom keys | Any `KEY=VALUE` is allowed; match `api_key_env` in `config.yaml` |

Example: profile has `api_key_env: ROUTERAI_API_KEY` → set `ROUTERAI_API_KEY=...` in `.env`.

Deprecated: inline `api_key` in YAML — use `.env` instead.

## active_profile

Name of the profile from `profiles` used for embeddings and search. After changing profile or `dimensions`, run `reindex` with `force: true`.

## profiles

Semantic search uses **embedding** models (vectorization), not chat LLMs. All cloud examples use OpenAI-compatible `/v1/embeddings`.

**Ready-made provider examples** (LM Studio, RouterAI, Alibaba DashScope) with Russian comments: [config.yaml](../config.yaml) — profiles `openai_local`, `lmstudio_qwen`, `routerai_bge_m3`, `routerai_openai_small`, `dashscope_beijing`, `dashscope_intl`. Switch via `active_profile`, then `reindex` with `force: true`.

| Field | Providers | Description |
|-------|-----------|-------------|
| `description` | all | Human-readable label |
| `provider` | all | `openai_compatible` or `onnx` |
| `model` | all | Model identifier |
| `dimensions` | all | Vector size (must match model output) |
| `base_url` | openai_compatible | API base, e.g. `http://127.0.0.1:1234/v1` |
| `api_key_env` | openai_compatible | Name of variable in `.env` |
| `api_key` | openai_compatible | Deprecated — use `.env` |
| `batch_size` | all | Indexing batch size (default 32) |
| `timeout_seconds` | openai_compatible | HTTP timeout |
| `extra_headers` | openai_compatible | Additional HTTP headers |
| `model_path` | onnx | Directory with `model_optimized.onnx` |

### Provider quick reference

| Profile | Provider | `base_url` | API key env |
|---------|----------|------------|-------------|
| `openai_local`, `lmstudio_qwen` | [LM Studio](https://lmstudio.ai/docs/developer/openai-compat/embeddings) | `http://127.0.0.1:1234/v1` | — |
| `routerai_bge_m3`, `routerai_openai_small` | [RouterAI](https://routerai.ru/) | `https://routerai.ru/api/v1` | `ROUTERAI_API_KEY` |
| `dashscope_beijing` | [Alibaba Model Studio](https://modelstudio.console.alibabacloud.com/) (Beijing) | `https://dashscope.aliyuncs.com/compatible-mode/v1` | `DASHSCOPE_API_KEY` |
| `dashscope_intl` | Alibaba Model Studio (International) | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | `DASHSCOPE_API_KEY` |

Docker → host LM Studio: `http://host.docker.internal:1234/v1`

### onnx (Phase 4)

See profile `local_multilingual` in [config.yaml](../config.yaml).

## indexing

| Key | Default | Description |
|-----|---------|-------------|
| `extensions` | see config.yaml | File extensions to index |
| `skip_dirs` | see config.yaml | Directories to skip |
| `lock_stale_seconds` | 300 | Reclaim stale `index.lock` |
| `stall_seconds` | 120 | No progress → recovery |
| `heartbeat_seconds` | 15 | Lock heartbeat interval |

Env override: `INDEXING_STALL_SECONDS` (stall detection / stale progress recovery).

## search

| Key | Default | Description |
|-----|---------|-------------|
| `slow_threshold_seconds` | 5 | Absolute slow search threshold |
| `degrade_ratio` | 2.0 | current > median × ratio |
| `stats_window` | 20 | Rolling metrics window |
| `stats_min_samples` | 5 | Min samples before degrade compare |

Env overrides: `SEARCH_SLOW_THRESHOLD_SECONDS`, `SEARCH_DEGRADE_RATIO`, `SEARCH_STATS_WINDOW`.

## file_watcher

| Key | Default | Description |
|-----|---------|-------------|
| `enabled` | true | Auto-reindex on file changes |
| `debounce_seconds` | 2 | Debounce after last event |
| `run_as_daemon` | false | Separate watcher process (Phase 3) |
| `backend` | auto | `auto`, `inotify`, `polling` |
| `poll_interval_seconds` | 10 | Polling interval |

On Windows Docker bind-mounts use `backend: polling`.

Env overrides: `FILE_WATCHER_ENABLED`, `FILE_WATCHER_BACKEND`, `FILE_WATCHER_POLL_INTERVAL_SECONDS`.

## logging

| Key | Default |
|-----|---------|
| `level` | INFO |
| `verbose` | false |
| `max_bytes` | 5242880 |
| `backup_count` | 3 |

Env overrides: `MCP_LOG_LEVEL`, `MCP_LOG_VERBOSE`, `MCP_LOG_MAX_BYTES`, `MCP_LOG_BACKUP_COUNT`. Logs: stderr + `data/logs/server.log`.

## server

| Key | Default | Description |
|-----|---------|-------------|
| `http_addr` | `:8080` | HTTP listen address |

Override with `HTTP_ADDR` env.

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKSPACE_ROOT` | cwd | Project root to index |
| `WORKSPACE_ID` | `WORKSPACE_ROOT` | Stable owner ID in index_meta |
| `INDEX_DIR` | `.mcp-semantic-search-zvec-go/data/index` | Index storage |
| `CONFIG_PATH` | `.mcp-semantic-search-zvec-go/config.yaml` | Config file |
| `AUTO_INDEX_ON_START` | false / true at install | Background index on start via `reindex` coordinator |
| `GITHUB_REPO` | `1CSerg/mcp-semantic-search-zvec-go` | For check_update |
| `HTTP_ADDR` | `:8080` | HTTP bind |
| `API_TOKEN` | — | HTTP Bearer auth (set in `.env`) |
| `ENV_PATH` | auto | Path to `.env` secrets file |

Planned (Phase 1+): `EMBEDDING_PROFILE` to override `active_profile` from yaml — not read in Phase 0 bootstrap.

## daemon.yaml (Phase 5)

Shared daemon registration:

```yaml
max_open_workspaces: 10

workspaces:
  - id: my-app
    root: /path/to/my-app
    index_dir: /path/to/my-app/.mcp-semantic-search-zvec-go/data/index
    config_path: /path/to/my-app/.mcp-semantic-search-zvec-go/config.yaml
```

Env alternative: `WORKSPACES_CONFIG=/path/to/daemon.yaml`.
