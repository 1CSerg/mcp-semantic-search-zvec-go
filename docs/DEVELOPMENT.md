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

Logs go to stderr. File logging — Phase 3.

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
  indexer/                          # Phase 2
docs/
scripts/                            # install
templates/                          # MCP fragments
```

## zvec-go (Phase 1)

Vector store uses community [zvec-go](https://github.com/danieleugenewilliams/zvec-go) (CGO).

Before integration:

1. Complete spike checklist in [ZVEC_SPIKE.md](ZVEC_SPIKE.md).
2. Build zvec native deps per zvec-go README (`make deps`).
3. Set `CGO_ENABLED=1` for builds that link zvec.

Collection schema (must match for index portability):

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

Pure Go stub (Phase 0): `GOOS=linux GOARCH=amd64 go build ...`

With zvec CGO (Phase 1+): use CI matrix with native runners per OS; avoid naive cross-compile from Windows to Linux with CGO.

## CI

- `.github/workflows/ci.yml` — `go test -race`, проверка покрытия (≥80% `./internal/...`), `go vet`, `golangci-lint` on push/PR
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

Минимум **80%** для `./internal/...` (встроенный `go tool cover`).

```bash
make test-cover          # отчёт по функциям
make test-cover-check    # fail если < 80%
```

Windows: `.\scripts\check-coverage.ps1`

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
