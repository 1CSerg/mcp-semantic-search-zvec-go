# mcp-semantic-search-zvec-go

Отказоустойчивый сервис **семантического поиска** по исходному коду и документации. Один Go-бинарник — два транспорта:

- **MCP stdio** — для Cursor, Roo Code и других MCP-хостов (агенты)
- **HTTP REST** — для любого другого ПО

Векторное хранилище: [zvec](https://github.com/alibaba/zvec) in-process. Эмбеддинги: OpenAI-compatible HTTP (Phase 1) и локальный ONNX (Phase 4).

## Quick start (разработка)

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go
cd mcp-semantic-search-zvec-go
go build -o bin/mcp-semantic-search-zvec-go ./cmd/mcp-semantic-search-zvec-go

# HTTP API
./bin/mcp-semantic-search-zvec-go --http
curl http://127.0.0.1:8080/health

# MCP stdio (для Cursor — см. docs/INSTALL.md)
./bin/mcp-semantic-search-zvec-go --stdio
```

## Установка в целевой проект

```powershell
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go $env:TEMP\mcp-semantic-search-zvec-go
& "$env:TEMP\mcp-semantic-search-zvec-go\scripts\install\install.ps1" -TargetRoot (Get-Location).Path
```

Подробнее: [docs/INSTALL.md](docs/INSTALL.md), [AGENTS.md](AGENTS.md).

## Документация

| Документ | Описание |
|----------|----------|
| [AGENTS.md](AGENTS.md) | MCP в целевом проекте: install, tools, env, troubleshooting |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | Разработка репозитория: сборка, тесты, CGO, zvec-go |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Архитектура, режимы развёртывания, отказоустойчивость |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Фазы разработки 0–5 |
| [docs/API.md](docs/API.md) | HTTP REST и MCP tools |
| [docs/CONFIG.md](docs/CONFIG.md) | config.yaml и переменные окружения |
| [docs/spike/ZVEC_SPIKE.md](docs/spike/ZVEC_SPIKE.md) | Gate Phase 1: проверка zvec-go |
| [docs/phases/PHASE3_RESULTS.md](docs/phases/PHASE3_RESULTS.md) | Gate evidence Phase 3 (resilience, watcher) |
| [docs/phases/PHASE4_RESULTS.md](docs/phases/PHASE4_RESULTS.md) | Gate evidence Phase 4 (local ONNX) |
| [docs/phases/PHASE5_RESULTS.md](docs/phases/PHASE5_RESULTS.md) | Gate evidence Phase 5 (shared daemon) |

## Режимы multi-project

| Режим | Описание |
|-------|----------|
| **Per-project** (по умолчанию) | Один процесс на workspace; изоляция индексов |
| **Shared daemon** (Phase 5) | Один HTTP-сервис, несколько workspace через `workspace_id` и `--stdio-proxy` |

## Статус

**v0.1.3** — MCP stdio + HTTP, полная индексация (Phase 2), watcher (Phase 3), локальный ONNX (Phase 4), shared daemon (Phase 5). Сборка с нативными deps: `make build-zvec` или install-скрипт.

## Лицензия

MIT — см. [docs/LICENSE](docs/LICENSE).
