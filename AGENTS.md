# Agent guide: mcp-semantic-search-zvec-go

> **Аудитория:** агент в **целевом проекте пользователя**, где MCP уже установлен (install, вызов tools, env, troubleshooting).
> Разработка самого Go-сервера (сборка, тесты, CGO): [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

Инструкция для агентов при установке и использовании MCP в **целевом проекте пользователя**.

## Что это

MCP-сервер семантического поиска (Go): zvec + HTTP/OpenAI-compatible embeddings. Отдельный продукт от `mcp-semantic-search-zvec` (Python). Install-каталог: `.mcp-semantic-search-zvec-go/`.

## Установка

1. Клонировать этот репозиторий (или скачать release).
2. Из **корня целевого проекта** запустить:

**Windows:**

```powershell
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go $env:TEMP\mcp-semantic-search-zvec-go
& "$env:TEMP\mcp-semantic-search-zvec-go\scripts\install\install.ps1" -TargetRoot (Get-Location).Path
```

**Linux / macOS:**

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go /tmp/mcp-semantic-search-zvec-go
TARGET_ROOT="$PWD" bash /tmp/mcp-semantic-search-zvec-go/scripts/install/install.sh
```

3. Перезапустить Cursor / Roo Code (MCP без hot-reload).

Скрипт создаёт `.mcp-semantic-search-zvec-go/config.yaml`, `.env` (секреты), бинарник, wiring в `.cursor/mcp.json` (прямой вызов `bin/mcp-semantic-search-zvec-go[.exe] --stdio`). Настройки — в `config.yaml`, API-ключи — в `.env`.

## MCP tools

| Tool | Назначение |
|------|------------|
| `semantic_search` | NL-запрос → ranked chunks |
| `index_status` | Пути, counts, progress индексации |
| `reindex` | Полная или инкрементальная переиндексация |
| `check_update` | Версия vs GitHub release |

**Правила для агента:**

- Исследование кодовой базы → сначала `semantic_search`, не полный обход репозитория.
- Статус индекса → `index_status`; не grep реализацию tools в исходниках MCP.
- При `indexing.running` — poll `index_status` до `idle`, затем повторить поиск.
- Не читать `.mcp-semantic-search-zvec-go/data/index/` целиком; при ошибках — tail `.mcp-semantic-search-zvec-go/data/logs/server.log`.

**Параметры `semantic_search`:**

- `limit` (integer, default 10) — число результатов; опционально `path_glob`.
- Пример: `{"query": "authentication middleware", "limit": 15}`.

## HTTP API

При запуске с `--http` (или `--http` вместе с `--stdio`):

```bash
curl http://127.0.0.1:8080/health
curl http://127.0.0.1:8080/v1/status
curl -X POST http://127.0.0.1:8080/v1/search -H "Content-Type: application/json" -d '{"query":"authentication"}'
```

См. [docs/API.md](docs/API.md).

## Переменные окружения

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKSPACE_ROOT` | cwd | Корень индексируемого проекта |
| `WORKSPACE_ID` | `WORKSPACE_ROOT` | Stable owner ID для index_meta |
| `INDEX_DIR` | `.mcp-semantic-search-zvec-go/data/index` | zvec + manifest |
| `CONFIG_PATH` | `.mcp-semantic-search-zvec-go/config.yaml` | YAML config |
| `AUTO_INDEX_ON_START` | `true` (install) | Фоновая индексация при старте |
| `HTTP_ADDR` | `:8080` | HTTP listen address |
| `API_TOKEN` | — | Optional Bearer token for HTTP (обычно в `.env`) |
| `ENV_PATH` | auto | Путь к файлу `.env` с секретами |

## Troubleshooting

| Symptom | Action |
|---------|--------|
| MCP not listed | Restart IDE; check `.cursor/mcp.json` JSON |
| Empty search, indexing idle | `reindex`; check `index_status` |
| `index_owner_mismatch` | Re-run install; separate `INDEX_DIR` per project |
| Windows file watcher misses saves | Set `file_watcher.backend: polling` in config |
| Shared daemon: `workspace_id` required | Use `--stdio-proxy --workspace-id=<id>` or HTTP `X-Workspace-ID` |
| Shared daemon: unknown workspace | Check `daemon.yaml` id matches proxy `--workspace-id` |

## Shared daemon (optional)

Default install uses per-project `--stdio`. For one daemon serving multiple projects:

1. Register workspaces in `daemon.yaml` (see [docs/CONFIG.md](docs/CONFIG.md)).
2. Run daemon with `--daemon --daemon-config …`.
3. Wire Cursor MCP with `--stdio-proxy --workspace-id=<id> --daemon-url=…`.

Per-project mode is unchanged and needs no daemon.

## Обновление

1. `check_update` MCP tool.
2. Re-run install script from updated clone/release.
3. Restart IDE.

## Uninstall

```powershell
& .\.mcp-semantic-search-zvec-go\uninstall.ps1
```

(Shell: `./.mcp-semantic-search-zvec-go/uninstall.sh` — Phase 2 install bundle.)
