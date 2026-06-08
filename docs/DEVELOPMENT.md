# Development

## Requirements

- Go 1.26+
- Git
- CGO toolchain (Phase 1+ for zvec-go): GCC on Linux, MSVC or MinGW on Windows
- Optional: golangci-lint, Docker

## Clone and build

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go
cd mcp-semantic-search-zvec-go
make setup-hooks   # один раз: git add на Windows без fatal LF/CRLF
go build -o bin/mcp-semantic-search-zvec-go ./cmd/mcp-semantic-search-zvec-go
go test ./...
```

Windows (PowerShell): `.\scripts\setup-git-hooks.ps1` вместо `make setup-hooks`.

Окончания строк (LF):

- `.gitattributes` (`eol=lf`) — при `git add` в индекс попадает LF (clean-фильтр Git).
- `setup-hooks` выставляет `core.autocrlf=false` и `core.safecrlf=false` **для этого репо** — без этого на Windows `git add` падает с `LF/CRLF would be replaced`.
- `git addnorm` (= `scripts/git-add.sh`) дополнительно приводит рабочую копию к LF перед индексацией. Обычный `git add` после `setup-hooks` тоже работает; встроенные `add`/`stage` через alias переопределить нельзя.

Dev mode uses repo `config.yaml` automatically when install tree is absent.

Секреты в dev: скопируйте `templates/env.example` в `.mcp-semantic-search-zvec-go/.env` или задайте `ENV_PATH`. Бинарник загружает `.env` при старте до чтения профилей.

## Run modes

```bash
# MCP stdio (Cursor spawns this)
./bin/mcp-semantic-search-zvec-go --stdio

# HTTP only
./bin/mcp-semantic-search-zvec-go --http --http-addr :8080

# Both (HTTP in background goroutine + MCP stdio)
./bin/mcp-semantic-search-zvec-go --stdio --http
```

Logs go to stderr and `.mcp-semantic-search-zvec-go/data/logs/server.log` (rotation via `logging.max_bytes` / `logging.backup_count`). Fatal panics write `data/logs/last_crash.json`.

## Project layout

```
cmd/mcp-semantic-search-zvec-go/   # main
internal/
  config/                           # YAML + env
  service/                          # Core API
  transport/mcp/                    # MCP stdio
  transport/http/                   # REST
  store/zvec/                       # Phase 1 — zvec-go
  embeddings/                     # Phase 1/4
  indexer/                          # scan, chunk, coordinator (Phase 2)
  watcher/                          # fsnotify + polling (Phase 3)
  logging/                          # file log rotation (Phase 3)
  crash/                            # last_crash.json (Phase 3)
docs/
scripts/                            # install
templates/                          # MCP fragments
```

## zvec-go (Phase 1)

Vector store uses official [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) v0.3.1 (CGO, vendor pre-built libs).

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

Clones [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) tag `v0.3.1` into `.deps/zvec-go` and downloads pre-built libs from GitHub Releases. `go.mod` uses `replace => ./.deps/zvec-go`.

### ONNX (Phase 4)

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

Windows: CGO via **WinLibs MinGW gcc** (recommended) or VS **clang-cl**; plain `cl` rejects cgo `-Werror`. Script:

```powershell
.\scripts\build-zvec-windows.ps1
```

`zvec_c_api.dll` must be next to the executable or on `PATH` (script copies to `bin/`).

**Spike in Docker (Linux amd64):**

```powershell
docker run --rm -v "${PWD}:/src" -w /src golang:1.26.3-bookworm bash /src/scripts/run-spike-docker-inner.sh
```

First run ~2–3 min (clone + download libs). Results: [SPIKE_RESULTS.md](SPIKE_RESULTS.md).

**Phase 1 gate smoke** (seed-index → HTTP search, mock embeddings):

```powershell
.\scripts\smoke-phase1.ps1
```

Linux: `make smoke-phase1`

**Phase 2 gate smoke** (empty project → reindex → HTTP search):

```powershell
.\scripts\smoke-phase2.ps1
```

Linux: `make smoke-phase2`

**Phase 3 gate smoke** (reconnect resilience, watcher, `/ready`, search metrics):

```powershell
.\scripts\smoke-phase3.ps1
```

Linux: `make smoke-phase3`

**Phase 4 gate smoke** (local ONNX `local_multilingual`, no external embedding API):

```powershell
.\scripts\smoke-phase4.ps1
```

Linux: `make smoke-phase4`

Production build:

```bash
make build-zvec
make seed-index
./bin/mcp-semantic-search-zvec-go --http
```

Spike checklist: [ZVEC_SPIKE.md](ZVEC_SPIKE.md).

### See also

- [zvec-go examples](https://github.com/zvec-ai/zvec-go/tree/main/examples) — Go API reference
- [zvec-agent-skills](https://github.com/zvec-ai/zvec-agent-skills) — product concepts (Python/Node); hybrid search ideas for Phase 2

Collection schema:

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

Windows: `.\scripts\check-coverage.ps1`

Переопределение: `COVERAGE_MIN=90 COVERAGE_PKG_MIN=60 make test-cover-check`

HTML-отчёт: `go tool cover -html=coverage.out -o coverage.html`

## Contributing workflow

1. Branch from `main`.
2. `go test ./...` && `make test-cover-check` && `go vet ./...` && `gofmt -w .`
3. Update docs if API/config changes.
4. PR with Russian commit messages (project convention) — см. `.cursor/rules/git-commits-ru.mdc`.

Конвенции для агентов в **этом репозитории**: `.cursor/rules/development.mdc` (краткие gate-правила).

Использование MCP в **целевом проекте пользователя** (install, tools, troubleshooting): [AGENTS.md](../AGENTS.md).

## Version bump

- Единственный источник версии: `internal/version/version.go`.
- `scripts/install.sh` и `scripts/install.ps1` читают версию из клона; install собирает бинарник (`go build`) или копирует готовый из `bin/`. Не добавляйте захардкоженный fallback в скрипты.
- Перед тегом `v*`: отредактируйте `version.go`; release workflow проверяет совпадение с git-тегом.
- Примеры установки в `AGENTS.md`, `README.md`, `docs/INSTALL.md` — из актуального клона или release-тега.
