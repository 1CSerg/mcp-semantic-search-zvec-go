# mcp-semantic-search-zvec-go

Отказоустойчивый сервис **семантического поиска** по исходному коду и документации. Один Go-бинарник — два транспорта:

- **MCP stdio** — для Cursor, Roo Code и других MCP-хостов (агенты)
- **HTTP REST** — для любого другого ПО

Векторное хранилище: [zvec](https://github.com/alibaba/zvec) in-process. Эмбеддинги: OpenAI-compatible HTTP и локальный ONNX.

## Quick start (разработка)

Первый `go build` без тегов — **stub** (без zvec/ONNX, поиск не работает). Для production-поведения: `make fetch-zvec-libs && make build-zvec` — см. [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go
cd mcp-semantic-search-zvec-go
go build -o bin/mcp-semantic-search-zvec-go ./cmd/mcp-semantic-search-zvec-go   # stub

# HTTP API (stub — health/status only)
./bin/mcp-semantic-search-zvec-go --http
curl http://127.0.0.1:8080/health

# MCP stdio (для Cursor — см. INSTALL.md)
./bin/mcp-semantic-search-zvec-go --stdio
```

## Установка в целевой проект

См. [INSTALL.md](INSTALL.md) и [AGENTS.md](AGENTS.md).

## Документация

| Документ | Описание |
|----------|----------|
| [AGENTS.md](AGENTS.md) | MCP в целевом проекте: install, tools, env, troubleshooting |
| [INSTALL.md](INSTALL.md) | Установка в целевой проект: prerequisites, Cursor wiring, Docker |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | Разработка репозитория: сборка, тесты, CGO, zvec-go |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Архитектура, режимы развёртывания, отказоустойчивость |
| [docs/API.md](docs/API.md) | HTTP REST и MCP tools |
| [docs/CONFIG.md](docs/CONFIG.md) | config.yaml и переменные окружения |

## Режимы multi-project

| Режим | Описание |
|-------|----------|
| **Per-project** (по умолчанию) | Один процесс на workspace; изоляция индексов |
| **Shared daemon** | Один HTTP-сервис, несколько workspace через `workspace_id` и `--stdio-proxy` |

## Статус

**v0.1.3** — MCP stdio + HTTP, полная индексация, watcher, локальный ONNX, shared daemon. Сборка с нативными deps: `make build-zvec` или install-скрипт.

## Лицензия

MIT — см. [docs/LICENSE](docs/LICENSE).
