# Realworld manual E2E harness

Manual-only end-to-end tests for the shipped binary (`zvec,onnx,treesitter`). **Not** part of `go test ./...` or CI.

## Quick start

```bash
make test-realworld
# LM Studio tier (skips if server down):
make test-realworld-lmstudio
# Include Docker smoke after Go scenarios:
bash scripts/realworld/run-all.sh --profile onnx --docker
```

Windows:

```powershell
.\scripts\realworld\run-all.ps1 -Profile onnx
.\scripts\realworld\run-all.ps1 -Profile onnx -Docker
.\scripts\realworld\setup-harness.ps1 -Profile onnx
go test -tags "realworld,zvec" -count=1 -timeout 30m -v ./tests/realworld/...
```

## Layout

| Path | Purpose |
|------|---------|
| `corpus/` | Git-tracked multi-language fixture tree with unique markers |
| `config/` | Profile templates (onnx, lmstudio, mock-*, daemon-workspace) |
| `harness/` | Go helpers (HTTP/MCP/daemon spawn, auth, locks, cleanup) |
| `scenarios/` | Scenario tests (`//go:build realworld && zvec`) |
| `.realworld/` | Ephemeral runtime tree (binary, index, config) — **gitignored** |

Runtime env (set by harness helpers):

- `WORKSPACE_ROOT=<repo>/tests/realworld/corpus`
- `INDEX_DIR=<repo>/.realworld/data/index`
- `CONFIG_PATH=<repo>/.realworld/config.yaml`
- `REALWORLD_PROFILE=onnx|lmstudio` (set by `run-all.*`)

## Scenario matrix

### MVP (wave 1)

| ID | Test | Description |
|----|------|-------------|
| T1 | `TestHTTPTransport`, `TestHTTPStatusEndpoints` | `--http`: health, status, reindex, search, `/ready` |
| T2 | `TestMCPStdioTransport` | `--stdio` subprocess: `index_status`, `reindex`, `semantic_search` |
| T7 | `TestInstallLayoutSmoke` | Install script: binary, launcher (Windows), `mcp.json`, managed Cursor rule, `.env` |
| I1 | (in T1/chunking) | Full `reindex force:true` over corpus |
| S1–S4 | `TestChunking*` | AST, prose, SDBL, line_window |
| E1–E2 | `TestEmbedServerDown`, `TestEmbedDimensionMismatch` | Embedding failures |

### Wave 2

| ID | Test | Description |
|----|------|-------------|
| T4 | `TestDaemonMultiWorkspaceLRU` | `--daemon`, 3 workspaces, LRU `max_open_workspaces: 2` |
| T5 | `TestMCPStdioProxyToDaemon` | `--stdio-proxy` → daemon search |
| T6 | *(manual)* | GUI + duplicate stdio — see [Manual checklist T6](#manual-checklist-t6-gui) |
| A1 | `TestDaemonOpenRedactsPaths` | Open daemon redacts paths in status |
| A2 | `TestDaemonBearerShowsFullPaths` | Bearer token → full paths |
| A3 | `TestPerProjectHTTPBearerAuth` | Per-project HTTP `API_TOKEN` reject/accept |
| C1 | `TestDuplicateStdioSuspected` | Two `--stdio` → `duplicate_stdio_suspected` |
| C2 | `TestSearchDuringIndexing` | Search while indexing (no panic) |
| C3 | `TestGracefulShutdownMidSearch` | SIGTERM mid-search, clean port release |
| C4 | `TestStaleZvecLockReclaim` | Stale zvec `LOCK` reclaimed on restart |
| I2 | `TestIncrementalFileChange` | Watcher polling → updated chunk |
| I3 | `TestIncrementalDeleteRenameNew` | Delete/rename/new file manifest purge |
| I4 | `TestInterruptIndexingResume` | kill -9 mid-index → resume |
| E3 | `TestLMStudioDownSkip` | LM Studio tier skip when server down |
| E4 | `TestEmbedTransientRetrySuccess` | Mock 503 × N then success (`max_retries`) |
| E5 | `TestEmbedMissingAPIKeyEnv` | Missing `api_key_env` value → clear error |
| S5 | `TestSearchPathGlob` | `path_glob: "**/*.go"` |
| S6 | `TestSemanticRankingAuthMiddleware` | Semantic ranking (ONNX); `TestLMStudioSemanticRanking` for LM tier |
| S7–S8 | `TestSearchEdge*`, `TestSearchBeforeReady*` | Bad query/JSON, before/during/after index |
| D1–D2 | `run-docker.sh` / `--docker` | Docker build + HTTP reindex/search |
| D3 | *(manual)* | Docker → host LM Studio — see [Manual checklist D3](#manual-checklist-d3-docker-lm-studio) |
| CLI1–CLI4 | `TestCLI*` | `--version`, `--config`, `--http-addr`, `--stop-stdio-for-workspace` |
| CLI5 | *(manual)* | No flags → GUI (Windows) / stdio — see [Manual checklist CLI5](#manual-checklist-cli5-no-flags) |
| Config | `TestEnvPathPrecedence`, `TestWorkspaceRootEnvOverride`, … | `ENV_PATH`, env overrides |
| Cyrillic | `TestCyrillicCorpusPath`, `TestCyrillicIndexDir` | Windows only (`//go:build windows`) |

## Profiles

- **onnx** (default): `local_multilingual` offline ONNX; requires `make fetch-onnx-model`.
- **lmstudio**: `lmstudio_qwen`; preflight checks `http://127.0.0.1:1234/v1/models`; sets `REALWORLD_PROFILE=lmstudio`.

Mock configs (isolated `tmp-index`): `mock-fail`, `mock-dim-mismatch`, `mock-retry`, `mock-api-key`.

## Selective runs

```bash
bash scripts/realworld/run-all.sh --profile onnx --run TestDaemon
bash scripts/realworld/run-all.sh --keep-index --run TestChunking
bash scripts/realworld/run-docker.sh   # D1/D2 only
```

## Manual checklist T6 (GUI)

Windows only; not automated (Fyne UI).

1. Run `scripts/realworld/setup-harness.ps1 -Profile onnx`.
2. Start MCP `--stdio` against `.realworld/` env (or let Cursor spawn it).
3. Launch GUI: `.realworld\bin\mcp-semantic-search-zvec-go.exe` (no flags).
4. Confirm warning about competing `--stdio` / `duplicate_stdio_suspected` in GUI or `index_status`.
5. Terminate competing stdio from GUI; verify incremental reindex starts.

## Manual checklist CLI5 (no flags)

| OS | Expected default |
|----|------------------|
| Windows | Desktop GUI opens |
| Linux/macOS | Blocks on `--stdio` (MCP mode) |

Verify manually after install; do not run in CI.

## Manual checklist D3 (Docker LM Studio)

1. Start LM Studio on host port `1234` with embedding model.
2. Run container with `base_url: http://host.docker.internal:1234/v1` in mounted config.
3. `reindex` + `semantic_search` inside container against corpus mount.

Document-only; optional follow-up automation.

## Prerequisites

- CGO toolchain + `make fetch-zvec-libs`
- ONNX: `make fetch-onnx-runtime` + model fetch (setup-harness runs these)
- Docker (optional): `run-docker.sh` / `--docker`
- Windows: native DLLs next to `.realworld/bin/mcp-semantic-search-zvec-go.exe`

## Troubleshooting

| Symptom | Action |
|---------|--------|
| Tests skipped: harness not ready | Run `scripts/realworld/setup-harness` |
| Empty search after reindex | Check `.realworld/logs/`; verify ONNX model path |
| LOCK / duplicate stdio | Ensure no stray MCP processes; restart IDE |
| LM Studio suite skipped | Start LM Studio on `:1234` with embedding model loaded |
| Watcher incremental flaky | `FILE_WATCHER_BACKEND=polling` (harness sets this for I2/I3) |
| Docker build slow | Use `--docker` only when needed; image caches layers |
