# mcp-semantic-search-zvec-go

**В процессе разработки**, могут быть ошибки.

Сервис **семантического поиска** по исходному коду и документации. Один headless Go-бинарник с двумя серверными транспортами и управлением проектами:

- **MCP stdio** — для Cursor, Roo Code и других MCP-хостов (агенты)
- **HTTP REST** — для любого другого ПО

Запуск без аргументов печатает справку и завершается с кодом 0: режим указывается явно (`--stdio`, `--http`, `--daemon`, `--stdio-proxy`). Нативного desktop-интерфейса в приложении нет.

Векторное хранилище: [zvec](https://github.com/alibaba/zvec) in-process. Эмбеддинги: OpenAI-compatible HTTP и локальный ONNX.

## Quick start (разработка)

Первый `go build` без тегов — **stub** (без zvec/ONNX, поиск не работает). Для production-поведения как в Release/install: `make fetch-zvec-libs && make build-zvec` (`-tags "zvec,onnx,treesitter"`). При `strategy: hybrid` shipped-бинарник индексирует `.md`/`.markdown`/`.mdc`/`.txt` через **prose** chunker; enabled code langs (`.go`, `.py`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.ts`, `.tsx`, **1C** `.bsl`/`.os` — AST cAST; `.dcs`/встроенные SDBL-строки — эвристика при `languages.bsl.include_sdbl: true`); остальные расширения — `line_window`. Legacy fallback без AST: `-tags "zvec,onnx"`; см. [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go
cd mcp-semantic-search-zvec-go
go build -o bin/mcp-semantic-search-zvec-go ./cmd/mcp-semantic-search-zvec-go   # stub

# Справка по режимам (то же, что запуск без аргументов)
./bin/mcp-semantic-search-zvec-go --help

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
| [INSTALL.md](INSTALL.md) | Установка: global layout (по умолчанию), legacy per-project, Cursor wiring, Docker, миграция, обновление, удаление |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | Разработка репозитория: сборка, тесты, CGO, zvec-go |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Архитектура, режимы развёртывания, отказоустойчивость |
| [docs/API.md](docs/API.md) | HTTP REST (включая `/v1/admin/*`) и MCP tools |
| [docs/CONFIG.md](docs/CONFIG.md) | config.yaml, daemon.yaml, project.yaml, переменные окружения |

## Режимы multi-project

| Режим | Описание |
|-------|----------|
| **Global install** (по умолчанию) | Одно приложение на пользователя, один общий daemon владеет всеми коллекциями; в проекте только `project.yaml` с `collection_id`; в IDE — лёгкий stdio-прокси |
| **Per-project** (legacy, явно) | Один процесс на workspace; изоляция индексов, exe и индекс внутри проекта |
| **Shared daemon** (legacy, вручную) | Один HTTP-сервис, несколько workspace через `workspace_id` и `--stdio-proxy --workspace-id` |

Кратко о режиме по умолчанию:

```text
<userdir>/                      # %LOCALAPPDATA%\mcp-semantic-search-zvec-go (или MCP_GLOBAL_DIR)
  bin/ config.yaml daemon.yaml .env auth.token projects.d/ data/collections/<id>/

<project>/.mcp-semantic-search-zvec-go/project.yaml   # schema_version + collection_id
<project>/.cursor/mcp.json                            # managed: <userdir>/bin/… --stdio-proxy --project-config <abs project.yaml>
```

```powershell
& "...\scripts\install\install.ps1" -TargetRoot (Get-Location).Path   # приложение + этот проект
& "...\scripts\install\install.ps1" -InstallAppOnly                   # только приложение
& "$env:LOCALAPPDATA\mcp-semantic-search-zvec-go\bin\mcp-semantic-search-zvec-go.exe" --project-add D:\other
```

Legacy-режим: `-McpMode Native` / `--mcp-mode native`. Подробности: [INSTALL.md](INSTALL.md#global-layout-default).

## Статус

**v0.3.0** — CI/release: version alignment with tag v0.3.0; zvec-go pin v0.6.0. **v0.2.0** — shared-daemon registry lifecycle (cold-open, graceful shutdown, HTTP 503 при закрытии), redaction путей в open daemon (status/search/reindex). **Hybrid chunking:** shipped Release/install — `-tags "zvec,onnx,treesitter"` (prose для `.md`/`.mdc`/`.txt`; AST для enabled code langs; legacy CI fallback `-tags "zvec,onnx"` → `line_window` на коде). **v0.1.5** — MCP stdio + HTTP, watcher, локальный ONNX, Unicode INDEX_DIR на Windows. Сборка с нативными deps: `make build-zvec` или install-скрипт.

## Лицензия

MIT — см. [docs/LICENSE](docs/LICENSE).
