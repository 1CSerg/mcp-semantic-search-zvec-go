# Development

## Requirements

- Go 1.26+
- Git
- CGO toolchain (for zvec-go): GCC on Linux, MSVC or MinGW on Windows
- Windows GUI builds use Fyne and may require the desktop CGO toolchain even without zvec tags
- Optional: golangci-lint, Docker

## Clone and build

Первый `go build` без тегов — **stub** (unit tests, health/status; без zvec и ONNX). Для семантического поиска: `make fetch-zvec-libs && make build-zvec` (`-tags "zvec,onnx,treesitter"` — то же, что GitHub Release и install). При `strategy: hybrid` shipped-бинарник индексирует `.md`/`.markdown`/`.mdc`/`.txt` через **prose** chunker; enabled code langs (`.go`, `.py`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.ts`, `.tsx`, `.bsl`, `.os`) — через AST cAST; остальные расширения — `line_window`.

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go
cd mcp-semantic-search-zvec-go
make setup-hooks   # один раз: git add на Windows без fatal LF/CRLF
go build -o bin/mcp-semantic-search-zvec-go ./cmd/mcp-semantic-search-zvec-go   # stub
go test ./...
```

Windows (PowerShell): `.\scripts\setup-git-hooks.ps1` вместо `make setup-hooks`.

Окончания строк (LF):

- `.gitattributes` (`eol=lf`) — при `git add` в индекс попадает LF (clean-фильтр Git).
- `setup-hooks` выставляет `core.autocrlf=false` и `core.safecrlf=false` **для этого репо** — без этого на Windows `git add` падает с `LF/CRLF would be replaced`.
- `git addnorm` (= `scripts/dev/git-add.sh`) дополнительно приводит рабочую копию к LF перед индексацией. Обычный `git add` после `setup-hooks` тоже работает; встроенные `add`/`stage` через alias переопределить нельзя.

Dev mode uses repo `config.yaml` automatically when install tree is absent.

Path containment (`INDEX_DIR` / `CONFIG_PATH` / `daemon.yaml`): `internal/config/paths.go`, env `MCP_PATH_CONTAINMENT`, tests `go test ./internal/config/... -run Containment`.

Секреты в dev: скопируйте `templates/env.example` в `.mcp-semantic-search-zvec-go/.env` или задайте `ENV_PATH`. Бинарник загружает `.env` при старте до чтения профилей.

## Run modes

Windows: no CLI flags → desktop GUI. Linux/macOS: no CLI flags → `--stdio` (per-project MCP). `--version` / `-version` prints version and exits.

```bash
# Windows desktop GUI (default on Windows)
./bin/mcp-semantic-search-zvec-go.exe
./bin/mcp-semantic-search-zvec-go.exe --gui

# MCP stdio (Cursor spawns this explicitly)
./bin/mcp-semantic-search-zvec-go --stdio

# HTTP only (default bind 127.0.0.1:8080)
./bin/mcp-semantic-search-zvec-go --http

# Both (HTTP in background goroutine + MCP stdio)
./bin/mcp-semantic-search-zvec-go --stdio --http
```

Per-project `--http` and Cursor `--stdio` both acquire `stdio.lock` on the same `INDEX_DIR`. **Do not** run a separate `./bin/mcp-semantic-search-zvec-go --http` process while Cursor MCP is active for the same workspace — use `--stdio --http` in one process, or stop Cursor MCP first.

```bash
# Shared daemon (multi-workspace HTTP; default bind :8080)
./bin/mcp-semantic-search-zvec-go --daemon --daemon-config /path/to/daemon.yaml --http-addr :8080

# MCP stdio proxy to shared daemon (Cursor multi-repo)
./bin/mcp-semantic-search-zvec-go --stdio-proxy --workspace-id=my-app --daemon-url=http://127.0.0.1:8080

# Override config path
./bin/mcp-semantic-search-zvec-go --stdio --config /path/to/config.yaml
```

GUI mode reuses the per-project service layer but does not start auto-indexing or the file watcher in the GUI process for a **new** empty index. The Windows GUI is localized in Russian. When Cursor already runs `--stdio` for the same workspace, the GUI finds the live MCP process (`FindStdioForWorkspace`, then `LiveHolder`), shows a warning with that PID instead of competing with the MCP process, and offers a button to terminate the competing process; after termination, the GUI starts an incremental reindex automatically. Stale PIDs in `stdio.lock` are not shown after reclaim.

If indexing was **interrupted** (GUI closed mid-run), the next GUI start reclaims orphan zvec `LOCK` files, migrates `progress.json` from `context canceled` to idle interrupted state, and automatically runs incremental `reindex` to continue (manifest skips already indexed files). Shutdown waits for the background indexer **and in-flight searches** before closing zvec to avoid orphan locks.

Full flag table: [API.md](API.md#cli-flags).

Logs go to stderr and `.mcp-semantic-search-zvec-go/logs/server.log` (rotation via `logging.max_bytes` / `logging.backup_count`). Fatal panics write `logs/last_crash.json` under the install dir.

## Project layout

```
cmd/mcp-semantic-search-zvec-go/   # main
internal/
  config/                           # YAML + env
  service/                          # Core API
  transport/mcp/                    # MCP stdio
  transport/http/                   # REST
  store/zvec/                       # zvec-go vector store
  embeddings/                       # openai_compatible, onnx
  indexer/                          # scan, chunk, coordinator
    chunk/                          # ChunkRouter, line_window, ProcessBatches
      prose/                        # Markdown/plain prose chunker (Phase 1e)
      ast/                          # cAST engine, tree-sitter (build tag treesitter)
  watcher/                          # fsnotify + polling
  logging/                          # file log rotation
  crash/                            # last_crash.json
  daemon/                           # multi-workspace registry (BorrowService, LRU, Close drain)
docs/
scripts/                            # install, fetch, dev, smoke, spike (see scripts/README.md)
templates/                          # MCP fragments
```

### Shared daemon (contributors)

`internal/daemon/registry.go` — lazy workspace open, LRU eviction, `Close()` drain. HTTP handlers in `internal/transport/http/server.go` call `redactIfOpenDaemon` when `daemon` mode runs without `API_TOKEN` (strips path fields, sanitizes path-bearing text in status/search/reindex JSON). Tests: `internal/transport/http/server_test.go` (`TestRedactDaemon*`, `TestDaemonMode*`).

## zvec-go

Vector store uses official [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) v0.5.1 (CGO, vendor pre-built libs; native core [alibaba/zvec](https://github.com/alibaba/zvec) bundled with that release). Где зафиксирована версия и как её менять — [Versions](#versions) (подраздел **zvec-go**).

### Build tags

| Command | Tags | Result |
|---------|------|--------|
| `go test ./...` | default (`!zvec`) | Stub store, no native deps |
| `make build-zvec` | `zvec,onnx,treesitter` | **Shipped** production binary (Release, install scripts): zvec + ONNX + tree-sitter; full hybrid (prose + AST for enabled langs) |
| `go build -tags "zvec,onnx"` or `zvec,!treesitter` | without `treesitter` | Legacy/fallback: prose + `line_window` for code when AST unavailable |
| `go test -tags "zvec,treesitter" ./internal/indexer/chunk/...` | `zvec,treesitter` | AST chunking tests (CGO + tree-sitter) |
| `make test-integration` | `integration,zvec` | Spike gate tests |

### Native deps (one-time per machine / after clean)

```bash
make fetch-zvec-libs
# writes .deps/zvec-lib.env with ZVEC_LIB_DIR
```

Clones [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) tag `v0.5.1` into `.deps/zvec-go` and downloads pre-built libs from GitHub Releases. `go.mod` uses `replace => ./.deps/zvec-go`.

### tree-sitter (hybrid AST chunking)

AST chunking uses [go-tree-sitter](https://github.com/tree-sitter/go-tree-sitter) **v0.25+** (ABI 15 for `tree-sitter-bsl`) with build tag **`treesitter`** (requires CGO, same toolchain as zvec-go). Registered grammars: **go**, **python**, **javascript**, **typescript**, **tsx** (`.jsx` → tsx parser), **bsl** (`.bsl`, `.os`; SDBL via heuristic chunker when `include_sdbl: true`). **Release/install binaries include `treesitter`** in production tags.

| Build | Tags | Behavior |
|-------|------|----------|
| Stub | (none) | No zvec, no AST; `ast.ChunkLanguage` returns `ErrNotImplemented` |
| Shipped / Release / install | `zvec,onnx,treesitter` | Prose extensions → prose; enabled code langs → AST cAST; `.dcs` query blocks when `bsl.include_sdbl`; others → `line_window` |
| Legacy fallback (CI) | `zvec,onnx` or `zvec,!treesitter` | Prose extensions → prose chunker; code extensions → `line_window` when AST unavailable |

Verify CGO + grammar linkage (spike gate):

```bash
bash scripts/fetch/fetch-tree-sitter-grammars.sh
# with zvec libs on PATH/LD_LIBRARY_PATH:
source .deps/zvec-lib.env
export CGO_ENABLED=1 LD_LIBRARY_PATH="$ZVEC_LIB_DIR:$LD_LIBRARY_PATH"
go test -tags "zvec,treesitter" ./internal/indexer/chunk/...
```

Windows (PowerShell):

```powershell
.\scripts\fetch\fetch-zvec-libs.ps1   # if not yet fetched
.\scripts\fetch\fetch-tree-sitter-grammars.ps1
# prepends ZVEC_LIB_DIR to PATH when .deps/zvec-lib.env exists
go test -tags "zvec,treesitter" ./internal/indexer/chunk/...
```

Grammars are vendored via Go modules (`tree-sitter-go`, `tree-sitter-python`, `tree-sitter-javascript`, `tree-sitter-typescript`, `tree-sitter-bsl` via `replace` → `github.com/alkoleft/tree-sitter-bsl`); fetch scripts only verify compile/link. CI runs `treesitter-chunk` on Linux and AST chunk tests on Windows (`test-windows` job).

**Hybrid chunk benchmark gate** (`internal/indexer/chunk/benchmark_test.go`, tags `zvec,onnx,treesitter`):

```bash
BENCH_CHECK=1 go test -tags "zvec,onnx,treesitter" -run TestBenchmarkHybridWithin2x ./internal/indexer/chunk/...
BENCH_CHECK=1 BENCH_FULL=1 go test -tags "zvec,onnx,treesitter" -run TestBenchmarkHybridWithin2x ./internal/indexer/chunk/...  # 1000/200/200 fixtures
python scripts/dev/generate-chunk-benchmark-fixtures.py /tmp/chunk-bench --go 1000 --tsx 200 --bsl 200
```

Gate compares hybrid vs `line_window` wall time and `HeapInuse` delta (limit 2×). CI `treesitter-chunk` runs the reduced set (50/10/10) in the main chunk test step, then a blocking `BENCH_FULL=1` gate on 1000/200/200 fixtures (hybrid must stay within 2× line_window).

### ONNX (local offline)

Local embeddings use [onnxruntime_go](https://github.com/yalue/onnxruntime_go) with build tag `onnx`.

```bash
make fetch-onnx-runtime   # .deps/onnxruntime.env
make fetch-onnx-model     # default bundle under .mcp-semantic-search-zvec-go/models/...
make build-zvec           # -tags "zvec,onnx,treesitter"
```

Env:

| Variable | Description |
|----------|-------------|
| `ONNXRUNTIME_SHARED_LIBRARY_PATH` | Path to `libonnxruntime.so` / `.dylib` / `.dll` |
| `ORT_LIB_DIR` | Directory containing ONNX Runtime library |
| `ONNXRUNTIME_VERSION` | Override runtime version in fetch scripts (default `1.26.0`) |
| `ONNX_MODEL_SHA256` | Optional checksum for `model_optimized.onnx` |
| `ONNX_TOKENIZER_SHA256` | Optional checksum for `tokenizer.json` |

Linux / macOS runtime:

```bash
source .deps/zvec-lib.env
export LD_LIBRARY_PATH="$ZVEC_LIB_DIR:$LD_LIBRARY_PATH"
```

Windows: CGO via **WinLibs MinGW gcc** (recommended) or VS **clang-cl**; plain `cl` rejects cgo `-Werror`. Scripts:

```powershell
.\scripts\dev\build-release.ps1      # production release (-ldflags -s -w)
.\scripts\dev\build-zvec-windows.ps1 # dev build (same tags, no strip)
```

Linux / macOS release: `make build-release` or `bash scripts/dev/build-release.sh`.

`zvec_c_api.dll` must be next to the executable or on `PATH` (script copies to `bin/`).

**Spike in Docker (Linux amd64):**

```powershell
docker run --rm -v "${PWD}:/src" -w /src golang:1.26.3-bookworm bash /src/scripts/spike/run-docker-inner.sh
```

First run ~2–3 min (clone + download libs). Covers integration checklist below.

**Zvec search smoke** (`make smoke-phase1` — seed-index → HTTP search, mock embeddings):

```powershell
.\scripts\smoke\run-phase1.ps1
```

Linux: `make smoke-phase1`

**Indexer smoke** (`make smoke-phase2` — empty project → reindex → HTTP search):

```powershell
.\scripts\smoke\run-phase2.ps1
```

Linux: `make smoke-phase2`

**Resilience smoke** (`make smoke-phase3` — reconnect, watcher, `/ready`, search metrics):

```powershell
.\scripts\smoke\run-phase3.ps1
```

Linux: `make smoke-phase3`

**ONNX smoke** (`make smoke-phase4` — local `local_multilingual`, no external embedding API):

```powershell
.\scripts\smoke\run-phase4.ps1
```

Linux: `make smoke-phase4`

**Shared daemon smoke** (`make smoke-phase5` — 3 workspaces, MCP proxy unit check):

```powershell
.\scripts\smoke\run-phase5.ps1
```

Linux: `make smoke-phase5`

**Windows multi-project smoke** (project-local bin, Cyrillic path, `mcp.json` wiring):

```powershell
.\scripts\smoke\run-mcp-staging-multi-windows.ps1
```

Makefile: `make smoke-staging-multi-windows`

Production build:

```bash
make build-zvec
make seed-index
./bin/mcp-semantic-search-zvec-go --http
```

### Integration checklist

Run on **Windows amd64** and **Linux amd64** after bump zvec-go or schema changes:

```bash
make fetch-zvec-libs
export CGO_ENABLED=1
# Linux/macOS: source .deps/zvec-lib.env && export LD_LIBRARY_PATH="$ZVEC_LIB_DIR:$LD_LIBRARY_PATH"
make test-integration
```

| # | Test | Pass criteria |
|---|------|---------------|
| 1 | Create collection with schema below | No CGO/link errors |
| 2 | Insert 100 docs with fp32 vectors | `doc_count` matches |
| 3 | Vector query top-k | Results ordered by score |
| 4 | Open existing collection read-only | Second open in same process idempotent |
| 5 | Delete by doc id | Count decreases |
| 6 | Graceful `Close()` then reopen | No LOCK file error |
| 7 | Kill -9 then stale lock handling | Next process reclaims (app-level) |
| 8 | Windows MSVC/MinGW build | Binary runs on target host |

Code: `internal/store/zvec/store.go`, tests in `store_integration_test.go` (`TestIntegrationSpikeChecklist`, tags `integration,zvec`). Windows Cyrillic path smoke: `store_cyrillic_integration_test.go` (`TestIntegrationCyrillicIndexDir`).

**Contributor note (Windows LOCK):** if zvec reports `Can't open lock file` or `Can't lock read-only collection`, investigate duplicate per-project MCP processes (`--stdio`, `--http`) and stale LOCK reclaim (`PrepareWorkspaceLocks`, `PrepareStdio`, `openZvecWithRecovery`) before assuming path encoding failure. **Do not** relocate zvec collections to `%LOCALAPPDATA%` or another ASCII-only root solely because `INDEX_DIR` contains Cyrillic characters.

| Outcome | Action |
|---------|--------|
| All pass | OK to ship zvec bump |
| Windows link fails | `zvec_c_api.dll` next to exe; see Windows CGO notes above |
| Schema incompatible | Indexes require `reindex` with `force: true` |
| Vendor libs GLIBC mismatch | Fallback: zvec-go source mode (`-tags source`) |

### See also

- [zvec-go examples](https://github.com/zvec-ai/zvec-go/tree/main/examples) — Go API reference
- [zvec-agent-skills](https://github.com/zvec-ai/zvec-agent-skills) — product concepts (Python/Node); hybrid search ideas for future work

Collection name: `ws_<sha256(workspace:profile:dims)[:16]>`. Fields:

| Field | Type |
|-------|------|
| `path` | string |
| `start_line` | int64 |
| `end_line` | int64 |
| `chunk_type` | string | `code` (default AST/line_window), **`markdown`** for prose (`.md`, `.markdown`, `.mdc`, `.txt`), or **`query`** for SDBL query text (heuristic chunker — not tree-sitter) |
| `name` | string |
| `snippet` | string |
| `symbol_name` | string |
| `symbol_kind` | string |
| `parent_scope` | string |
| `chunk_strategy` | string |
| `embedding` | vector fp32, N = profile dimensions |

Hybrid AST chunks populate `symbol_*`, `parent_scope`, and `chunk_strategy` (`ast`, `partial`, or `line_window`). **Prose** chunks (`.md`, `.markdown`, `.mdc`, `.txt`) set `chunk_type: markdown`, `chunk_strategy: prose` or `partial`, `symbol_kind` e.g. `section`, `paragraph` — works without `treesitter`. SDBL query chunks (`.dcs` `<query>` blocks, embedded BSL strings) set `chunk_type: query` and `symbol_kind: query` via the **heuristic** SDBL chunker — BSL itself uses **tree-sitter-bsl**. Legacy indexes may leave symbol fields empty until `reindex` with `force: true`.

## Cross-compile

Default `go build` (no tags): stub semantic backend without zvec/ONNX. Linux/macOS keep this path pure Go; Windows also builds the Fyne GUI and may need the normal Fyne desktop toolchain.

With zvec (`-tags zvec`): use native runners per OS; avoid naive cross-compile with CGO.

## CI

- `.github/workflows/ci.yml` — `go test -race`, покрытие (≥80% `./internal/...`, ≥50% на пакет), `go vet`, job `zvec-integration` (`-tags integration,zvec`), `test-windows`, golangci-lint (blocking) on push/PR
- **Matrix (OS × build tags):** stub (no tags), `zvec,onnx` / `zvec,!treesitter`, `zvec,onnx,treesitter` on Ubuntu/Windows; optional `macos-latest` treesitter job (`continue-on-error`)
- **`treesitter-chunk` job:** AST chunk tests, blocking `BENCH_FULL=1` hybrid benchmark gate (1000/200/200 fixtures, ≤2× `line_window`), **≥80% coverage** on `internal/indexer/chunk` and `internal/indexer/chunk/ast` with `-tags "zvec,onnx,treesitter"`
- **`merge-config` job:** `python -m unittest scripts/install/merge-config_test.py`
- `.github/workflows/release.yml` — tag `v*` → binaries + Docker

## Testing MCP locally

Use [MCP Inspector](https://github.com/modelcontextprotocol/inspector) or Cursor after wiring `.cursor/mcp.json`.

Quick HTTP smoke:

```bash
go run ./cmd/mcp-semantic-search-zvec-go --http &
curl -s http://127.0.0.1:8080/health | jq .
curl -s http://127.0.0.1:8080/v1/status | jq .
```

## Покрытие тестами

Пороги для `./internal/...` (встроенный `go tool cover`):

- **80%** — суммарно по проекту (`COVERAGE_MIN`)
- **50%** — минимум на каждый Go-пакет (`COVERAGE_PKG_MIN`)

```bash
make test-cover          # отчёт по функциям
make test-cover-check    # fail если ниже порогов
```

Windows: `.\scripts\dev\check-coverage.ps1`

Переопределение: `COVERAGE_MIN=90 COVERAGE_PKG_MIN=60 make test-cover-check`

HTML-отчёт: `go tool cover -html=coverage.out -o coverage.html`

## Contributing workflow

1. Branch from `main`.
2. `go test ./...` && `make test-cover-check` && `go vet ./...` && `gofmt -w .` (CI также гоняет `go test` на `windows-latest` без `-race`).
3. При изменении install/config merge: `python -m unittest scripts/install/merge-config_test.py` (нужен `pip install -r scripts/install/requirements.txt`).
4. Update docs if API/config changes.
5. PR with Russian commit messages (project convention) — см. `.cursor/rules/git-commits-ru.mdc`.

Конвенции для агентов в **этом репозитории**: `.cursor/rules/development.mdc` (gate-правила и MCP dogfooding при планировании/ревью).

Использование MCP в **целевом проекте пользователя** (install, tools, troubleshooting): [AGENTS.md](../AGENTS.md).

## Realworld harness (manual E2E)

Расширенные E2E-сценарии «как в проде» — **не** входят в `go test ./...`, CI и pre-commit.

```bash
make test-realworld              # ONNX offline (local_multilingual)
make test-realworld-lmstudio     # lmstudio_qwen; skip если LM Studio недоступен
bash scripts/realworld/run-all.sh --profile onnx --run TestChunking
bash scripts/realworld/run-all.sh --profile onnx --docker   # + Docker D1/D2 smoke
```

Windows: `.\scripts\realworld\run-all.ps1 -Profile onnx` (флаг `-Docker` для контейнерного smoke).

Prerequisites: `make build-zvec` (CGO, zvec libs, ONNX runtime); ONNX model подтягивается `setup-harness`. Ephemeral install tree: `.realworld/` (gitignored). Корпус: `tests/realworld/corpus/`. Wave 2: daemon/proxy, auth, concurrency, incremental lifecycle, search edge cases, CLI flags, Cyrillic paths (Windows), optional Docker. Manual-only: GUI duplicate stdio (T6), no-flags default (CLI5), Docker→LM Studio (D3). Подробнее: [tests/realworld/README.md](../tests/realworld/README.md).

## Versions

### MCP server

- Единственный источник версии: `internal/version/version.go`.
- `scripts/install/install.sh` и `scripts/install/install.ps1` читают версию из клона; install собирает бинарник (`go build`) или копирует готовый из `bin/`. Не добавляйте захардкоженный fallback в скрипты.
- Перед тегом `v*`: отредактируйте `version.go`; release workflow проверяет совпадение с git-тегом.
- Примеры установки в `AGENTS.md`, `README.md`, `INSTALL.md` — из актуального клона или release-тега.

### zvec-go

Версия [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) задаётся в нескольких местах — при bump обновляйте **все** перечисленные:

| Место | Что менять |
|-------|------------|
| `go.mod` | `require github.com/zvec-ai/zvec-go vX.Y.Z` и `replace => ./.deps/zvec-go` |
| `internal/version/version.go` | `ZvecGoVersion` (тег `vX.Y.Z`, вшит в бинарник) |
| `scripts/fetch/fetch-zvec-libs.sh` | default `ZVEC_GO_TAG` (сейчас `v0.5.1`) |
| `scripts/fetch/fetch-zvec-libs.ps1` | то же |
| `docker/Dockerfile` | `ARG ZVEC_GO_TAG=vX.Y.Z` |

Локальная копия модуля и pre-built native libs (`zvec_c_api.dll` / `libzvec_c_api.so` и т.д.) подтягиваются в `.deps/zvec-go` через `make fetch-zvec-libs` по тегу из fetch-скриптов. Переопределение без правки файлов: env `ZVEC_GO_TAG=vX.Y.Z`. Fetch-скрипты при несовпадении тега делают `git fetch` + `checkout -f` (re-clone вручную не обязателен) и накатывают ACP-патч Unicode-путей из [`scripts/fetch/patches/zvec-go-acp/`](../scripts/fetch/patches/zvec-go-acp/) (Windows Cyrillic `INDEX_DIR`).

После смены версии: обновите таблицу выше, при необходимости перегенерируйте `collection.go.patch`, и снова `make fetch-zvec-libs`.

**Автомиграция в целевом проекте:** при старте бинарник сравнивает `version.ZvecGoVersion` с `zvec_go_version` в `index_meta.json`. При расхождении сбрасывает zvec-коллекцию и `manifest.db`; если `AUTO_INDEX_ON_START=true` (Native install, per-project `--stdio`) — запускает force `reindex`, иначе нужен ручной MCP `reindex`. В shared daemon режиме auto-index при старте не действует.
