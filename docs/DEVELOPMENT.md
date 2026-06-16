# Development

## Requirements

- Go 1.26+
- Git
- CGO toolchain (for zvec-go): GCC on Linux, MSVC or MinGW on Windows
- Optional: golangci-lint, Docker

## Clone and build

Первый `go build` без тегов — **stub** (unit tests, health/status; без zvec и ONNX). Для семантического поиска: `make fetch-zvec-libs && make build-zvec`.

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

No CLI flags → `--stdio` (per-project MCP). `--version` / `-version` prints version and exits.

```bash
# MCP stdio (Cursor spawns this; default when no flags)
./bin/mcp-semantic-search-zvec-go --stdio

# HTTP only (default bind 127.0.0.1:8080)
./bin/mcp-semantic-search-zvec-go --http

# Both (HTTP in background goroutine + MCP stdio)
./bin/mcp-semantic-search-zvec-go --stdio --http

# Shared daemon (multi-workspace HTTP; default bind :8080)
./bin/mcp-semantic-search-zvec-go --daemon --daemon-config /path/to/daemon.yaml --http-addr :8080

# MCP stdio proxy to shared daemon (Cursor multi-repo)
./bin/mcp-semantic-search-zvec-go --stdio-proxy --workspace-id=my-app --daemon-url=http://127.0.0.1:8080

# Override config path
./bin/mcp-semantic-search-zvec-go --stdio --config /path/to/config.yaml
```

Full flag table: [API.md](API.md#cli-flags).

Logs go to stderr and `.mcp-semantic-search-zvec-go/data/logs/server.log` (rotation via `logging.max_bytes` / `logging.backup_count`). Fatal panics write `data/logs/last_crash.json`.

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
  watcher/                          # fsnotify + polling
  logging/                          # file log rotation
  crash/                            # last_crash.json
  daemon/                           # multi-workspace registry
docs/
scripts/                            # install, fetch, dev, smoke, spike (see scripts/README.md)
templates/                          # MCP fragments
```

## zvec-go

Vector store uses official [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) v0.5.0 (CGO, vendor pre-built libs; native core [alibaba/zvec](https://github.com/alibaba/zvec) ≥ v0.4.0). Где зафиксирована версия и как её менять — [Versions](#versions) (подраздел **zvec-go**).

### Build tags

| Command | Tags | Result |
|---------|------|--------|
| `go test ./...` | default (`!zvec`) | Stub store, no native deps |
| `make build-zvec` | `zvec,onnx` | Production binary with zvec + local ONNX |
| `make test-integration` | `integration,zvec` | Spike gate tests |

### Native deps (one-time per machine / after clean)

```bash
make fetch-zvec-libs
# writes .deps/zvec-lib.env with ZVEC_LIB_DIR
```

Clones [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) tag `v0.5.0` into `.deps/zvec-go` and downloads pre-built libs from GitHub Releases. `go.mod` uses `replace => ./.deps/zvec-go`.

### ONNX (local offline)

Local embeddings use [onnxruntime_go](https://github.com/yalue/onnxruntime_go) with build tag `onnx`.

```bash
make fetch-onnx-runtime   # .deps/onnxruntime.env
make fetch-onnx-model     # default bundle under .mcp-semantic-search-zvec-go/models/...
make build-zvec           # -tags "zvec,onnx"
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

Code: `internal/store/zvec/store.go`, tests in `store_integration_test.go` (`TestIntegrationSpikeChecklist`, tags `integration,zvec`).

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
| `chunk_type` | string |
| `name` | string |
| `snippet` | string |
| `embedding` | vector fp32, N = profile dimensions |

## Cross-compile

Default `go build` (no tags): pure Go stub — useful for CI coverage without CGO.

With zvec (`-tags zvec`): use native runners per OS; avoid naive cross-compile with CGO.

## CI

- `.github/workflows/ci.yml` — `go test -race`, покрытие (≥88% `./internal/...`, ≥50% на пакет), `go vet`, job `zvec-integration` (`-tags integration,zvec`), golangci-lint on push/PR
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

- **88%** — суммарно по проекту (`COVERAGE_MIN`)
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
2. `go test ./...` && `make test-cover-check` && `go vet ./...` && `gofmt -w .`
3. При изменении install/config merge: `python -m unittest scripts/install/merge-config_test.py` (нужен `pip install -r scripts/install/requirements.txt`).
4. Update docs if API/config changes.
5. PR with Russian commit messages (project convention) — см. `.cursor/rules/git-commits-ru.mdc`.

Конвенции для агентов в **этом репозитории**: `.cursor/rules/development.mdc` (краткие gate-правила).

Использование MCP в **целевом проекте пользователя** (install, tools, troubleshooting): [AGENTS.md](../AGENTS.md).

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
| `scripts/fetch/fetch-zvec-libs.sh` | default `ZVEC_GO_TAG` (сейчас `v0.5.0`) |
| `scripts/fetch/fetch-zvec-libs.ps1` | то же |

Локальная копия модуля и pre-built native libs (`zvec_c_api.dll` / `libzvec_c_api.so` и т.д.) подтягиваются в `.deps/zvec-go` через `make fetch-zvec-libs` по тегу из fetch-скриптов. Переопределение без правки файлов: env `ZVEC_GO_TAG=vX.Y.Z`. Fetch-скрипты при несовпадении тега делают `git fetch` + `checkout` (re-clone вручную не обязателен).

После смены версии: обновите таблицу выше и снова `make fetch-zvec-libs`.

**Автомиграция в целевом проекте:** при старте бинарник сравнивает `version.ZvecGoVersion` с `zvec_go_version` в `index_meta.json`. При расхождении сбрасывает zvec-коллекцию и `manifest.db`; если `AUTO_INDEX_ON_START=true` (Native install, per-project `--stdio`) — запускает force `reindex`, иначе нужен ручной MCP `reindex`. В shared daemon режиме auto-index при старте не действует.
