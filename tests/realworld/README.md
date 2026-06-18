# Realworld manual E2E harness

Manual-only end-to-end tests for the shipped binary (`zvec,onnx,treesitter`). **Not** part of `go test ./...` or CI.

## Quick start

```bash
make test-realworld
# LM Studio tier (skips if server down):
make test-realworld-lmstudio
```

Windows:

```powershell
.\scripts\realworld\run-all.ps1 -Profile onnx
.\scripts\realworld\setup-harness.ps1 -Profile onnx
go test -tags "realworld,zvec" -count=1 -timeout 30m -v ./tests/realworld/...
```

## Layout

| Path | Purpose |
|------|---------|
| `corpus/` | Git-tracked multi-language fixture tree with unique markers |
| `config/` | Profile templates (onnx, lmstudio, mock-fail, mock-dim-mismatch) |
| `harness/` | Go helpers (`RequireHarness`, HTTP/MCP spawn, assertions) |
| `scenarios/` | Scenario tests (`//go:build realworld && zvec`) |
| `.realworld/` | Ephemeral runtime tree (binary, index, config) — **gitignored** |

Runtime env (set by harness helpers):

- `WORKSPACE_ROOT=<repo>/tests/realworld/corpus`
- `INDEX_DIR=<repo>/.realworld/data/index`
- `CONFIG_PATH=<repo>/.realworld/config.yaml`

## MVP scenario matrix

| ID | Test | Description |
|----|------|-------------|
| T1 | `TestHTTPTransport`, `TestHTTPStatusEndpoints` | `--http`: health, status, reindex, search, `/ready` |
| T2 | `TestMCPStdioTransport` | `--stdio` subprocess: `index_status`, `reindex`, `semantic_search` |
| T7 | `TestInstallLayoutSmoke` | Install script: binary, launcher (Windows), `mcp.json`, managed Cursor rule, `.env` |
| I1 | (in T1/chunking) | Full `reindex force:true` over corpus |
| S1 | `TestChunkingASTLanguages` | AST hits for Go, Python, JS/JSX/TSX, BSL |
| S2 | `TestChunkingProse` | Prose chunks for `.md`, `.markdown`, `.mdc`, `.txt` |
| S3 | `TestChunkingSDBLQuery` | `.dcs` → `chunk_type: query` |
| S4 | `TestChunkingLineWindow` | `.sql` → `line_window` |
| E1 | `TestEmbedServerDown` | Unreachable embed API → clear indexing error |
| E2 | `TestEmbedDimensionMismatch` | Profile dims ≠ mock response → dimension mismatch |

## Profiles

- **onnx** (default): `local_multilingual` offline ONNX; requires `make fetch-onnx-model`.
- **lmstudio**: `lmstudio_qwen`; preflight checks `http://127.0.0.1:1234/v1/models`.

## Selective runs

```bash
bash scripts/realworld/run-all.sh --profile onnx --run TestHTTP
bash scripts/realworld/run-all.sh --keep-index --run TestChunking
```

## Prerequisites

- CGO toolchain + `make fetch-zvec-libs`
- ONNX: `make fetch-onnx-runtime` + model fetch (setup-harness runs these)
- Windows: native DLLs next to `.realworld/bin/mcp-semantic-search-zvec-go.exe`

## Troubleshooting

| Symptom | Action |
|---------|--------|
| Tests skipped: harness not ready | Run `scripts/realworld/setup-harness` |
| Empty search after reindex | Check `.realworld/logs/`; verify ONNX model path |
| LOCK / duplicate stdio | Ensure no stray MCP processes; restart IDE |
| LM Studio suite skipped | Start LM Studio on `:1234` with embedding model loaded |

## Second wave (not in MVP)

Daemon/proxy (T4–T5), GUI duplicate stdio (T6), incremental watcher (I2–I3), concurrency (C1–C4), Docker (D1–D2), auth (A1–A3), CLI flags, Cyrillic paths.
