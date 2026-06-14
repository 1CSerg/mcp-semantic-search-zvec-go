# Agent guide: mcp-semantic-search-zvec-go

> **Аудитория:** агент в **целевом проекте пользователя**, где MCP уже установлен (install, вызов tools, env, troubleshooting).
> Разработка самого Go-сервера (сборка, тесты, CGO): [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

Инструкция для агентов при установке и использовании MCP в **целевом проекте пользователя**.

## Что это

MCP-сервер семантического поиска (Go): zvec + HTTP/OpenAI-compatible embeddings. Отдельный продукт от `mcp-semantic-search-zvec` (Python). Install-каталог: `.mcp-semantic-search-zvec-go/`.

## Установка

Полные команды (Windows/Linux), prerequisites и структура каталогов: [docs/INSTALL.md](docs/INSTALL.md).

Кратко: clone репозиторий → `install.ps1` / `install.sh` из корня целевого проекта → перезапустить Cursor / Roo Code (MCP без hot-reload).

## MCP tools

Схемы аргументов: [docs/API.md](docs/API.md).

| Tool | Назначение |
|------|------------|
| `semantic_search` | NL-запрос → ranked chunks |
| `index_status` | Пути, counts, progress индексации |
| `reindex` | Полная или инкрементальная переиндексация |
| `check_update` | Установленная версия (**stub** — GitHub Releases не опрашивается) |

**Правила для агента:**

- Исследование кодовой базы → сначала `semantic_search`, не полный обход репозитория.
- Статус индекса → `index_status`; не grep реализацию tools в исходниках MCP.
- При `indexing.running` результаты `semantic_search` могут быть неполными; в ответе есть поле `indexing` с прогрессом.
- Не читать `.mcp-semantic-search-zvec-go/data/index/` целиком; при ошибках — tail `.mcp-semantic-search-zvec-go/data/logs/server.log`.

**Параметры `semantic_search`:**

- `limit` (integer, default 10) — число результатов; опционально `path_glob`.
- Пример: `{"query": "authentication middleware", "limit": 15}`.

## HTTP API

Справочник эндпоинтов и примеры curl: [docs/API.md](docs/API.md).

## Переменные окружения

Полная таблица: [docs/CONFIG.md](docs/CONFIG.md#environment-variables).

Для агента: `WORKSPACE_ROOT`, `INDEX_DIR`, `CONFIG_PATH`, `ENV_PATH`. Native install также задаёт `AUTO_INDEX_ON_START=true` в `.cursor/mcp.json` — см. таблицу режимов ниже.

## Per-project vs shared daemon

| Режим | Auto-index при старте |
|-------|----------------------|
| `--stdio` (Native install) | `AUTO_INDEX_ON_START=true` из install |
| `--stdio-proxy` + shared daemon | **нет** — вызвать MCP `reindex` вручную |

## Troubleshooting

| Symptom | Action |
|---------|--------|
| MCP not listed | Restart IDE; check `.cursor/mcp.json` JSON |
| Cursor MCP **error**, tools=0, но `exe --version` работает | Windows: re-run `install.ps1 -TargetRoot`; проверить `%APPDATA%\Cursor\logs\*\mcpprocess.log` (`ENOENT`, `non-retryable`); kill McpProcess + restart Cursor; см. [INSTALL.md](docs/INSTALL.md#cursor-mcp-error-на-windows) |
| После **переноса** проекта на Windows | Re-run `install.ps1 -TargetRoot` (обновляет `mcp.json` env и launcher paths) |
| **Несколько репо**, два окна Cursor | Native install OK (отдельный `.mcp-semantic-search-zvec-go/` на проект). Multi-root в одном окне → shared daemon |
| Docker + несколько репо | `docker-compose.daemon.yml` + `-McpMode Proxy` в install.ps1 на Windows |
| Empty search, indexing idle | `reindex`; check `index_status` |
| After MCP binary update (zvec-go bump) | Index resets on first start; in **Native** mode with `AUTO_INDEX_ON_START=true` reindex runs automatically, else call MCP `reindex` |
| `index_owner_mismatch` | Call MCP `reindex` with `force: true`. In **Native** mode with `AUTO_INDEX_ON_START=true` this runs on next start; keep separate `INDEX_DIR` per project if you share one clone |
| Windows file watcher misses saves | Set `file_watcher.backend: polling` in config |
| Shared daemon: `workspace_id` required | Use `--stdio-proxy --workspace-id=<id>` or HTTP `X-Workspace-ID` |
| Shared daemon: unknown workspace | Check `daemon.yaml` id matches proxy `--workspace-id` |

## Shared daemon (optional)

По умолчанию — per-project `--stdio`. Для нескольких проектов в одном окне Cursor: `daemon.yaml` → `--daemon` → `--stdio-proxy`.

Подробнее: [docs/INSTALL.md](docs/INSTALL.md#shared-daemon-phase-5), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/CONFIG.md](docs/CONFIG.md#daemonyaml-phase-5).

**Windows proxy:** `install.ps1 -McpMode Proxy -WorkspaceId <id> -DaemonUrl http://127.0.0.1:8080`.

## Обновление

1. Сравнить `index_status.server_version` или `bin/mcp-semantic-search-zvec-go --version` с [GitHub Releases](https://github.com/1CSerg/mcp-semantic-search-zvec-go/releases) (`check_update` — stub, не опрашивает GitHub).
2. Re-run install script from updated clone/release.
3. Restart IDE.

Merge `config.yaml`, `-ReplaceConfig`: [docs/INSTALL.md](docs/INSTALL.md#update--повторный-install).

## Uninstall

Команды и `-KeepData`: [docs/INSTALL.md](docs/INSTALL.md#uninstall).
