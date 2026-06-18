# Agent guide: mcp-semantic-search-zvec-go

> **Аудитория:** агент в **целевом проекте пользователя**, где MCP уже установлен (install, вызов tools, env, troubleshooting).
> Разработка самого Go-сервера (сборка, тесты, CGO): [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

Инструкция для агентов при установке и использовании MCP в **целевом проекте пользователя**.

## Что это

MCP-сервер семантического поиска (Go): zvec + HTTP/OpenAI-compatible embeddings. Отдельный продукт от `mcp-semantic-search-zvec` (Python). Install-каталог: `.mcp-semantic-search-zvec-go/`.

## Установка

Полные команды (Windows/Linux), prerequisites и структура каталогов: [INSTALL.md](INSTALL.md).

Кратко: clone репозиторий → `install.ps1` / `install.sh` из корня целевого проекта → перезапустить Cursor / Roo Code (MCP без hot-reload).

Install также создаёт Cursor rule [`.cursor/rules/semantic-search-zvec-go.mdc`](.cursor/rules/semantic-search-zvec-go.mdc) и Roo/Zoo Code rule [`.roo/rules/semantic-search-zvec-go.md`](.roo/rules/semantic-search-zvec-go.md) (English) с инструкциями по MCP tools; uninstall удаляет только install-managed файлы с маркером `managedBy: mcp-semantic-search-zvec-go`. Install также обновляет `.cursor/mcp.json` и `.roo/mcp.json`.

## MCP tools

Схемы аргументов: [docs/API.md](docs/API.md).

| Tool | Назначение |
|------|------------|
| `semantic_search` | NL-запрос → ranked chunks |
| `index_status` | Пути, counts, progress индексации |
| `reindex` | Полная или инкрементальная переиндексация |
| `check_update` | Сравнение с последним GitHub Release (кэш успеха 1 ч, ошибки 1 мин; отключить: `CHECK_UPDATE_DISABLE=true`; stub-сборка без zvec — заглушка) |

**Правила для агента:**

- Исследование кодовой базы → сначала `semantic_search`, не полный обход репозитория.
- **План или ревью** (code review, аудит, план рефакторинга) → **до** составления плана: `index_status`, затем несколько `semantic_search` по темам (concurrency, errors, security, indexing, transport и т.д.). Не заменять MCP на Task/explore, широкий grep или полный обход файлов, если MCP доступен.
- **Реализация** → тоже начинать с `semantic_search` для связанного кода; Read/Grep — после semantic hits или для точечных правок.
- Статус индекса → `index_status`; не grep реализацию tools в исходниках MCP.
- При `indexing.running` результаты `semantic_search` могут быть неполными; в ответе есть поле `indexing` с прогрессом.
- Не читать `.mcp-semantic-search-zvec-go/data/index/` целиком; при ошибках — tail `.mcp-semantic-search-zvec-go/logs/server.log`.
- Пустой поиск при idle индексации → `reindex`; повторить поиск перед fallback на broad exploration.

**Параметры `semantic_search`:**

- `limit` (integer, default 10) — число результатов; опционально `path_glob`.
- Пример: `{"query": "authentication middleware", "limit": 15}`.

**Поля результатов** (после hybrid reindex; в legacy-индексе могут быть пустыми): `symbol_name`, `symbol_kind`, `parent_scope`, `chunk_strategy` — см. [docs/API.md](docs/API.md#post-v1search). `snippet` — исходный код без embed-префикса.

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
| MCP not listed | Restart IDE; check `.cursor/mcp.json` or `.roo/mcp.json` JSON |
| `config load failed: mapping values are not allowed` | Quote scalar values containing `:` in `config.yaml` (e.g. `description: "Foo: Bar"`) |
| Cursor MCP **error**, tools=0, но `exe --version` работает | Windows: re-run `install.ps1 -TargetRoot`; проверить `%APPDATA%\Cursor\logs\*\mcpprocess.log` (`ENOENT`, `non-retryable`); kill McpProcess + restart Cursor; см. [INSTALL.md](INSTALL.md#cursor-mcp-error-на-windows) |
| После **переноса** проекта на Windows | Re-run `install.ps1 -TargetRoot` (обновляет `mcp.json` env и launcher paths) |
| **Несколько репо**, два окна Cursor | Native install OK (отдельный `.mcp-semantic-search-zvec-go/` на проект). Multi-root в одном окне → shared daemon |
| Docker + несколько репо | `docker-compose.daemon.yml` + `-McpMode Proxy` в install.ps1 на Windows |
| Empty search, indexing idle | `reindex`; check `index_status` |
| `embedding failed: dimension mismatch: got N want M` | Check `dimensions` in the active profile matches the model (MRL models need server v0.1.8+ which sends `dimensions` to the API); run `reindex` with `force: true` after fixing |
| `index_status.message`: workspace path changed (misleading) | Check `identity_mismatch_reason` — often profile/dimensions/**chunking_version** changed, not path; `reindex` with `force: true` |
| Hybrid chunking: пустые `symbol_*` / `chunk_strategy` в поиске | Shipped binary без `treesitter` индексирует `.go` через `line_window`; для AST-полей — `treesitter`-сборка + `reindex` `force: true`. Смена `chunking.strategy` / `chunking.version` / `languages.*` — тоже `reindex` `force: true` |
| `identity_mismatch` / `chunking_version mismatch` | MCP `reindex` with `force: true`. Native `AUTO_INDEX_ON_START=true` — на следующем старте; shared daemon — вручную |
| `zvec_open_ok: false`, LOCK error, `zvec_doc_count: 0` | Duplicate `--stdio` processes — restart Cursor; check `index_status.diagnostics`; re-run install; `reindex` with `force: true` if needed |
| `--stdio` остался после закрытия Cursor/launcher | Сервер завершится сам, если исчезнет родительская цепочка запуска (`powershell`/`cmd` или Cursor). Если закрыт только workspace, но Cursor держит launcher живым, нужен Restart MCP/IDE или `--stop-stdio-for-workspace`. Для отладки: `MCP_DISABLE_PARENT_WATCH=true` |
| After MCP binary update (zvec-go bump) | Index resets on first start; in **Native** mode with `AUTO_INDEX_ON_START=true` reindex runs automatically, else call MCP `reindex` |
| `index_owner_mismatch` | Call MCP `reindex` with `force: true`. In **Native** mode with `AUTO_INDEX_ON_START=true` this runs on next start; keep separate `INDEX_DIR` per project if you share one clone |
| Windows file watcher misses saves | Set `file_watcher.backend: polling` in config |
| Shared daemon: `workspace_id` required | Use `--stdio-proxy --workspace-id=<id>` or HTTP `X-Workspace-ID` |
| Shared daemon: unknown workspace | Check `daemon.yaml` id matches proxy `--workspace-id` |
| Proxy / daemon without `API_TOKEN` | `index_status` omits `workspace_root`, `index_dir`, `current_file`, `failed_files`; set `API_TOKEN` on daemon + Bearer on HTTP for full paths |
| Daemon shutdown: `503 registry is closing` | Expected while daemon stops; retry after restart or wait for in-flight requests to finish |

## Shared daemon (optional)

Install по умолчанию настраивает per-project `--stdio` через явный launcher. На Windows запуск exe без ключей открывает GUI. Для нескольких проектов в одном окне Cursor: `daemon.yaml` → `--daemon` → `--stdio-proxy`.

Подробнее: [INSTALL.md](INSTALL.md#shared-daemon), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/CONFIG.md](docs/CONFIG.md#daemonyaml-phase-5).

**Windows proxy:** `install.ps1 -McpMode Proxy -WorkspaceId <id> -DaemonUrl http://127.0.0.1:8080`.

## Обновление

1. Сравнить `index_status.server_version` или `bin/mcp-semantic-search-zvec-go --version` с [GitHub Releases](https://github.com/1CSerg/mcp-semantic-search-zvec-go/releases) или вызвать MCP `check_update` (опрашивает GitHub, кроме stub-сборки).
2. Re-run install script from updated clone/release.
3. Restart IDE.

Merge `config.yaml`, `-ReplaceConfig`: [INSTALL.md](INSTALL.md#update--повторный-install).

## Uninstall

Команды и `-KeepData`: [INSTALL.md](INSTALL.md#uninstall).
