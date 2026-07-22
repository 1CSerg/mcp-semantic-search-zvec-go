# mcp-semantic-search-zvec-go

Отказоустойчивый сервис **семантического поиска** по исходному коду и документации. Один Go-бинарник — desktop GUI на Windows и два серверных транспорта:

- **Windows GUI** — окно для статуса индексации, переиндексации, поиска и просмотра результатов
- **MCP stdio** — для Cursor, Roo Code и других MCP-хостов (агенты)
- **HTTP REST** — для любого другого ПО

Векторное хранилище: [zvec](https://github.com/alibaba/zvec) in-process. Эмбеддинги: OpenAI-compatible HTTP и локальный ONNX.

## Quick start (разработка)

Первый `go build` без тегов — **stub** (без zvec/ONNX, поиск не работает). Для production-поведения как в Release/install: `make fetch-zvec-libs && make build-zvec` (`-tags "zvec,onnx,treesitter"`). При `strategy: hybrid` shipped-бинарник индексирует `.md`/`.markdown`/`.mdc`/`.txt` через **prose** chunker; enabled code langs (`.go`, `.py`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.ts`, `.tsx`, **1C** `.bsl`/`.os` — AST cAST; `.dcs`/встроенные SDBL-строки — эвристика при `languages.bsl.include_sdbl: true`); остальные расширения — `line_window`. Legacy fallback без AST: `-tags "zvec,onnx"`; см. [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go
cd mcp-semantic-search-zvec-go
go build -o bin/mcp-semantic-search-zvec-go ./cmd/mcp-semantic-search-zvec-go   # stub

# Windows GUI: .\bin\mcp-semantic-search-zvec-go.exe

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

**v0.3.0** — CI/release: version alignment with tag v0.3.0; zvec-go pin v0.5.1 (upstream v0.6.0 unavailable). **v0.2.0** — shared-daemon registry lifecycle (cold-open, graceful shutdown, HTTP 503 при закрытии), redaction путей в open daemon (status/search/reindex). **Hybrid chunking:** shipped Release/install — `-tags "zvec,onnx,treesitter"` (prose для `.md`/`.mdc`/`.txt`; AST для enabled code langs; legacy CI fallback `-tags "zvec,onnx"` → `line_window` на коде). **v0.1.5** — MCP stdio + HTTP, watcher, локальный ONNX, Unicode INDEX_DIR на Windows. Сборка с нативными deps: `make build-zvec` или install-скрипт.

## Лицензия

MIT — см. [docs/LICENSE](docs/LICENSE).
