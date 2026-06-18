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

## YAML pitfalls

Scalar values that contain a colon (`:`) must be wrapped in double quotes, or the YAML parser fails with `mapping values are not allowed in this context`:

```yaml
# Wrong — breaks parse:
description: RouterAI — Perplexity: Embed V1 4B

# Correct:
description: "RouterAI — Perplexity: Embed V1 4B"
```

On parse failure, the server adds a hint when the error mentions `mapping values`. Install merge (`merge-config.py`) warns about unquoted `description:` lines with colons.

## profiles

Semantic search uses **embedding** models (vectorization), not chat LLMs. All cloud examples use OpenAI-compatible `/v1/embeddings`.

**Ready-made provider examples** (LM Studio, RouterAI, Alibaba DashScope) with Russian comments: [config.yaml](../config.yaml) — profiles `openai_local`, `lmstudio_qwen`, `routerai_bge_m3`, `routerai_openai_small`, `routerai_perplexity_4b`, `dashscope_beijing`, `dashscope_intl`. Switch via `active_profile`, then `reindex` with `force: true`.

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
| `max_retries` | openai_compatible | Total HTTP attempts per embed batch on transient 429/5xx (default 3) |
| `retry_base_ms` | openai_compatible | Initial backoff between retries in ms (default 500, exponential) |
| `extra_headers` | openai_compatible | Additional HTTP headers |
| `model_path` | onnx | Directory with `model_optimized.onnx` |

### Provider quick reference

| Profile | Provider | `base_url` | API key env |
|---------|----------|------------|-------------|
| `openai_local`, `lmstudio_qwen` | [LM Studio](https://lmstudio.ai/docs/developer/openai-compat/embeddings) | `http://127.0.0.1:1234/v1` | — |
| `routerai_bge_m3`, `routerai_openai_small`, `routerai_perplexity_4b` | [RouterAI](https://routerai.ru/) | `https://routerai.ru/api/v1` | `ROUTERAI_API_KEY` |
| `dashscope_beijing` | [Alibaba Model Studio](https://modelstudio.console.alibabacloud.com/) (Beijing) | `https://dashscope.aliyuncs.com/compatible-mode/v1` | `DASHSCOPE_API_KEY` |
| `dashscope_intl` | Alibaba Model Studio (International) | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | `DASHSCOPE_API_KEY` |

Docker → host LM Studio: `http://host.docker.internal:1234/v1`

### MRL embedding models (Matryoshka)

Some OpenAI-compatible models support a `dimensions` request parameter (MRL). The server sends `profile.dimensions` in the embed API when set. The vector length in the response must match the configured value.

| Model | Default dim | MRL range | Recommendation |
|-------|-------------|-----------|----------------|
| `perplexity/pplx-embed-v1-4B` | 2560 | 128–2560 | `dimensions: 1024` for large repos; `2560` for max accuracy |
| `perplexity/pplx-embed-v1-0.6B` | 1024 | 128–1024 | `dimensions: 1024` |

After changing `active_profile` or `dimensions`, run `reindex` with `force: true`.

### onnx (local offline)

Profile `local_multilingual` in [config.yaml](../config.yaml) runs fully offline after the model bundle is downloaded.

| Item | Value |
|------|-------|
| Provider | `onnx` |
| Model bundle dir | `model_path` in profile (default `.mcp-semantic-search-zvec-go/models/paraphrase-multilingual-MiniLM-L12-v2`) |
| Required files | `model_optimized.onnx`, `tokenizer.json` |
| Dimensions | Must match model output (384 for default bundle) |
| Runtime | ONNX Runtime shared library next to binary or via `ONNXRUNTIME_SHARED_LIBRARY_PATH` / `ORT_LIB_DIR` |

Download model bundle:

```powershell
.\scripts\fetch-onnx-model.ps1 -DestDir .\.mcp-semantic-search-zvec-go\models\paraphrase-multilingual-MiniLM-L12-v2
```

```bash
bash scripts/fetch/fetch-onnx-model.sh .mcp-semantic-search-zvec-go/models/paraphrase-multilingual-MiniLM-L12-v2
```

Install with model fetch (when `active_profile: local_multilingual` or explicit flag):

```powershell
& scripts\install\install.ps1 -FetchONNXModel
```

```bash
FETCH_ONNX_MODEL=1 bash scripts/install/install.sh
```

Production build tags: `-tags "zvec,onnx"`. Default unit tests use stub ONNX (`go test ./...`); smoke/release use production tags.

## indexing

Tables use two default columns: **Code fallback** (key omitted in YAML) and **Install template** (repo [config.yaml](../config.yaml) merged on first install).

| Key | Code fallback | Install template | Description |
|-----|---------------|------------------|-------------|
| `extensions` | see config.yaml | see config.yaml | File extensions to index |
| `skip_dirs` | see config.yaml | see config.yaml | Directories to skip |
| `lock_stale_seconds` | 300 | 30 | Legacy/diagnostic stale hint for lock files (`isStale()`); reclaim uses OS lock |
| `stall_seconds` | 120 | 120 | No progress → recovery |
| `heartbeat_seconds` | 15 | 15 | **Deprecated** — ignored for indexing; kept for config compatibility |
| `max_file_bytes` | 2097152 | 2097152 | Max file size to index (bytes); `0` = no limit |
| `stream_chunk_threshold_bytes` | 262144 | 262144 | Above this size, use streaming line chunker |
| `max_line_bytes` | 1048576 | 1048576 | Max single line length (bytes); `0` = no limit |

### indexing.chunking

| Key | Code fallback | Description |
|-----|---------------|-------------|
| `strategy` | `hybrid` | `hybrid` or `line_window` (legacy slideWindow) |
| `version` | `1` | Chunking schema version stored in `index_meta.json`; mismatch triggers `identity_mismatch` |
| `size_metric` | `tokens` | Chunk size unit |
| `min_chunk_tokens` | `10` | Minimum chunk size (tokens) |
| `prose_overlap_ratio` | `0.12` | Overlap ratio for prose regions |
| `context_prefix` | `false` | Prefix parent scope to chunk text |
| `line_window.window_lines` | `40` | Legacy line window size |
| `line_window.overlap_lines` | `8` | Legacy line overlap |
| `languages` | go, python, js/ts, bsl | Per-language AST chunking toggles (`bsl.include_sdbl`) |

Env overrides: `CHUNKING_STRATEGY`, `CHUNKING_VERSION`.

### profiles: embed budget

| Key | ONNX default | openai_compatible default | Description |
|-----|--------------|---------------------------|-------------|
| `max_input_tokens` | **256** | 512 | Max tokens for chunk body before embedding |
| `embed_budget_ratio` | 0.90 | 0.50 | Fraction of `max_input_tokens` used for chunk body |

Env override for active profile: `EMBED_MAX_INPUT_TOKENS` (positive integer).

When `indexing.chunking.strategy: hybrid`, each profile must have `max_input_tokens > 0` after defaults and env overrides.

Env overrides: `INDEXING_STALL_SECONDS`, `INDEXING_MAX_FILE_BYTES`, `INDEXING_STREAM_CHUNK_THRESHOLD_BYTES`, `INDEXING_MAX_LINE_BYTES`.

## search

| Key | Default | Description |
|-----|---------|-------------|
| `slow_threshold_seconds` | 5 | Absolute slow search threshold |
| `degrade_ratio` | 2.0 | current > median × ratio |
| `stats_window` | 20 | Rolling metrics window |
| `stats_min_samples` | 5 | Min samples before degrade compare |

Env overrides: `SEARCH_SLOW_THRESHOLD_SECONDS`, `SEARCH_DEGRADE_RATIO`, `SEARCH_STATS_WINDOW`.

## file_watcher

| Key | Code fallback | Install template | Description |
|-----|---------------|------------------|-------------|
| `enabled` | false | true | Auto-reindex on file changes |
| `debounce_seconds` | 2 | 2 | Debounce after last event |
| `run_as_daemon` | false | false | Not implemented — ignored; watcher runs in-process |
| `backend` | auto | auto | `auto`, `inotify`, `polling` |
| `poll_interval_seconds` | 10 | 10 | Polling interval |

On Windows Docker bind-mounts use `backend: polling`.

Env overrides: `FILE_WATCHER_ENABLED`, `FILE_WATCHER_BACKEND`, `FILE_WATCHER_POLL_INTERVAL_SECONDS`.

## logging

| Key | Code fallback | Install template |
|-----|---------------|------------------|
| `level` | INFO | DEBUG |
| `verbose` | false | true |
| `max_bytes` | 5242880 | 1048576 |
| `backup_count` | 3 | 1 |

Env overrides: `MCP_LOG_LEVEL`, `MCP_LOG_VERBOSE`, `MCP_LOG_MAX_BYTES`, `MCP_LOG_BACKUP_COUNT`. Logs: stderr + `.mcp-semantic-search-zvec-go/logs/server.log` (relative to install dir, not under `data/`).

## server

| Key | Default | Description |
|-----|---------|-------------|
| `http_addr` | `127.0.0.1:8080` (per-project); `:8080` (daemon/Docker) | HTTP listen address |

Override with `HTTP_ADDR` env. Per-project default binds loopback only; use `:8080` explicitly for LAN access.

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKSPACE_ROOT` | cwd | Project root to index |
| `WORKSPACE_ID` | `WORKSPACE_ROOT` | Stable owner ID in index_meta |
| `INDEX_DIR` | `.mcp-semantic-search-zvec-go/data/index` | Index storage |
| `CONFIG_PATH` | `.mcp-semantic-search-zvec-go/config.yaml` | Config file |
| `AUTO_INDEX_ON_START` | false (code); `true` in Native install `mcp.json` | Background index on start via `reindex` coordinator. **Per-project `--stdio` only** — shared daemon workspaces ignore this env; call `reindex` manually |
| `GITHUB_REPO` | `1CSerg/mcp-semantic-search-zvec-go` | GitHub repo for `check_update` (polls Releases API) |
| `CHECK_UPDATE_DISABLE` | false | Set `true` to skip GitHub release polling in `check_update` |
| `HTTP_ADDR` | `127.0.0.1:8080` (per-project); `:8080` (daemon/Docker) | HTTP bind |
| `API_TOKEN` | — | HTTP Bearer auth. **Recommended** for shared daemon and `--stdio-proxy` when the daemon listens beyond loopback; without it, daemon HTTP redacts workspace paths in status/search/reindex (see [API.md](API.md#open-daemon-redaction)). Set in workspace or daemon `.env` |
| `ENV_PATH` | auto | Path to `.env` secrets file |
| `MCP_PATH_CONTAINMENT` | `warn` | Path validation for `INDEX_DIR` / `CONFIG_PATH`: `strict` (fail startup), `warn` (log only), `off` (disable) |
| `INDEXING_MAX_FILE_BYTES` | from yaml (`2097152`) | Override `indexing.max_file_bytes` (positive integer) |
| `INDEXING_STREAM_CHUNK_THRESHOLD_BYTES` | from yaml (`262144`) | Override `indexing.stream_chunk_threshold_bytes` (positive integer) |
| `INDEXING_MAX_LINE_BYTES` | from yaml (`1048576`) | Override `indexing.max_line_bytes` (positive integer) |
| `CHUNKING_STRATEGY` | from yaml (`hybrid`) | Override `indexing.chunking.strategy` (`hybrid`, `line_window`) |
| `CHUNKING_VERSION` | from yaml (`1`) | Override `indexing.chunking.version` (integer) |
| `EMBED_MAX_INPUT_TOKENS` | from yaml / provider default | Override `max_input_tokens` on **active** profile (positive integer) |
| `MANIFEST_WAL` | `auto` | SQLite manifest journal: `auto` (WAL off on cloud-sync paths), `on`, `off` |
| `MCP_CRASH_REDACT_PATHS` | `true` | Redact absolute paths in `last_crash.json` stack |
| `MCP_PROXY_LOG_DIR` | temp subdir | Crash report dir for `--stdio-proxy` |
| `MCP_DAEMON_LOG_DIR` / `LOG_DIR` | `./logs` | Crash/log dir for shared daemon |

Planned: `EMBEDDING_PROFILE` to override `active_profile` from yaml — not implemented yet.

### Path containment

Resolved `INDEX_DIR` and `CONFIG_PATH` must normally stay under `WORKSPACE_ROOT`. External index locations (e.g. Docker bind-mount outside project root) require an explicit allowlist in `daemon.yaml` (`path_allowlist`). Set `MCP_PATH_CONTAINMENT=strict` in per-project mode for fail-fast hardening.

## daemon.yaml (shared daemon)

Shared daemon registration. Template: [templates/daemon.yaml](../templates/daemon.yaml).

```yaml
max_open_workspaces: 10
path_containment: warn   # strict | warn | off
path_allowlist:          # optional; paths outside workspace root (Docker index mounts)
  - /workspaces/ws-alpha-index

workspaces:
  - id: my-app
    root: /path/to/my-app
    index_dir: /path/to/my-app/.mcp-semantic-search-zvec-go/data/index
    config_path: /path/to/my-app/.mcp-semantic-search-zvec-go/config.yaml
```

| Key | Default | Description |
|-----|---------|-------------|
| `max_open_workspaces` | 10 | LRU cap on concurrently opened workspace handles |
| `path_containment` | `warn` | Validate `index_dir` / `config_path` against workspace `root` |
| `path_allowlist` | — | Absolute paths permitted outside `root` (typically external `index_dir` mounts) |
| `workspaces[].id` | — | Stable workspace ID (used in HTTP/MCP proxy) |
| `workspaces[].root` | — | Project root (`WORKSPACE_ROOT`) |
| `workspaces[].index_dir` | `<root>/.mcp-…/data/index` | Index storage |
| `workspaces[].config_path` | `<root>/.mcp-…/config.yaml` | Per-workspace YAML config |

Run daemon:

```bash
.mcp-semantic-search-zvec-go/bin/mcp-semantic-search-zvec-go --daemon --daemon-config /path/to/daemon.yaml --http-addr :8080
```

Set `API_TOKEN` in the daemon process environment (or daemon host `.env`) when exposing the daemon on a network interface; pass `Authorization: Bearer <token>` from HTTP clients and configure proxy launchers accordingly.

Env alternative: `WORKSPACES_CONFIG=/path/to/daemon.yaml`.

Cursor MCP proxy wiring: [templates/cursor-mcp-proxy.fragment.json](../templates/cursor-mcp-proxy.fragment.json) — replace `MY_WORKSPACE_ID` with the workspace `id` from `daemon.yaml`.

Secrets remain per-project in each workspace `.env`; daemon loads them without mutating global process env.
