# Agent guide: mcp-semantic-search-zvec-go

> **Аудитория:** агент в **целевом проекте пользователя**, где MCP уже установлен (install, вызов tools, env, troubleshooting).
> Разработка самого Go-сервера (сборка, тесты, CGO): [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).
> Все планы по разработке сохранять: [docs/plans/](docs/plans/)
> Реализованные (выполненные и проверенные) планы переносить в [docs/plans/completed/](docs/plans/completed/), отметив в самом плане статус и дату завершения. В `docs/plans/` остаются только активные и ещё не реализованные планы.

Инструкция для агентов при установке и использовании MCP в **целевом проекте пользователя**.

## Что это

MCP-сервер семантического поиска (Go): zvec + HTTP/OpenAI-compatible embeddings. Отдельный продукт от `mcp-semantic-search-zvec` (Python). Install-каталог: `.mcp-semantic-search-zvec-go/`.

**Раскладка по умолчанию — global install:** одно приложение на пользователя в `%LOCALAPPDATA%\mcp-semantic-search-zvec-go` (или `MCP_GLOBAL_DIR`), один общий daemon владеет всеми коллекциями. Прежняя раскладка (exe, индекс и `.env` внутри проекта) остаётся доступной: `-McpMode Native` / `--mcp-mode native`.

## Установка

Полные команды (Windows/Linux), prerequisites и структура каталогов: [INSTALL.md](INSTALL.md).

Кратко: clone репозиторий → `install.ps1` / `install.sh` из корня целевого проекта → перезапустить Cursor / Roo Code (MCP без hot-reload). Установка приложения и добавление проекта — разные операции: `-InstallAppOnly` / `--install-app-only` ставит только приложение, проект добавляется позже через `--project-add <root>`.

Что появляется в проекте в раскладке по умолчанию (global install):

```text
<project>/.mcp-semantic-search-zvec-go/project.yaml    # schema_version + collection_id
<project>/.cursor/mcp.json                             # managed: <userdir>/bin/... --stdio-proxy --project-config <abs>
<project>/.cursor/rules/semantic-search-zvec-go.mdc
<project>/.roo/mcp.json
<project>/.roo/rules/semantic-search-zvec-go.md
```

В проекте нет exe, native libraries, моделей, индекса, общего `config.yaml` и секретов: профили и секреты живут только в user-level каталоге. `project.yaml` переносим (без абсолютных путей); абсолютные пути в IDE JSON допустимы.

## Команды управления проектами

Один и тот же установленный бинарник (`<userdir>/bin/mcp-semantic-search-zvec-go[.exe]`):

| Команда | Назначение |
|---------|------------|
| `--project-add <root>` | Зарегистрировать проект, поднять daemon при необходимости, перечитать registry |
| `--project-remove <root>` | Снять регистрацию и managed-файлы; данные коллекции сохраняются |
| `--project-new-collection <root>` | Дать копии проекта собственный `collection_id` |
| `--project-relocate <root> --yes` | Перенести проект, сохранив коллекцию (отказ, если коллекция открыта) |
| `--projects-reload` | Перечитать `projects.d` без перезапуска daemon |
| `--ensure-daemon` / `--stop-daemon` | Запустить / штатно остановить общий daemon |

Конфликты не разрешаются молча: тот же id на другом root — отказ (нужен `--project-new-collection`), тот же root с другим id — отказ (нужен `--project-remove`).

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

**Поля результатов** (после hybrid reindex; в legacy-индексе могут быть пустыми): `symbol_name`, `symbol_kind`, `parent_scope`, `chunk_strategy` — используйте их для понимания контекста (функция, секция markdown, SDBL query и т.д.); см. [docs/API.md](docs/API.md#post-v1search). Prose (`.md`, `.markdown`, `.mdc`, `.txt`): `chunk_strategy: prose`, `symbol_kind` e.g. `section`/`paragraph` — без `treesitter`. `snippet` — исходный код без embed-префикса.

**После upgrade** на версию с hybrid chunking (или смены `chunking.version` / `treesitter` binary) → MCP `reindex` с `force: true` (install-скрипт выводит предупреждение).

## HTTP API

Справочник эндпоинтов и примеры curl: [docs/API.md](docs/API.md).

## Переменные окружения

Полная таблица: [docs/CONFIG.md](docs/CONFIG.md#environment-variables).

Для агента: `MCP_GLOBAL_DIR` (global install), `WORKSPACE_ROOT`, `INDEX_DIR`, `CONFIG_PATH`, `ENV_PATH` (legacy per-project). Native install задаёт `AUTO_INDEX_ON_START=true` в `.cursor/mcp.json`; global install не передаёт secrets и пути через MCP env — см. таблицу режимов ниже.

## Режимы и auto-index

| Режим | Auto-index при старте |
|-------|----------------------|
| Global install (`--stdio-proxy --project-config`) | `auto_index_on_open: true` в `<userdir>/daemon.yaml` (по умолчанию): daemon строит/досинхронизирует коллекцию при первом открытии |
| `--stdio` (Native install) | `AUTO_INDEX_ON_START=true` из install |
| `--stdio-proxy --workspace-id` + shared daemon | **нет** — вызвать MCP `reindex` вручную |

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
| Hybrid chunking: пустые `symbol_*` / `chunk_strategy` в поиске | Legacy index или legacy binary (`-tags "zvec,onnx"` без `treesitter`): код → `line_window`; `.md`/`.markdown`/`.mdc`/`.txt` → prose. Shipped Release/install включает `treesitter`; после upgrade или смены `chunking.strategy` / `chunking.version` / `languages.*` / `prose_overlap_ratio` — `reindex` `force: true` |
| 1C BSL: нет `procedure`/`function` в поиске; `.dcs` только `line_window` | Shipped install — `-tags "zvec,onnx,treesitter"`; проверьте `languages.bsl.enabled: true`; для SDBL — `include_sdbl: true` (эвристика). Legacy binary без `treesitter` → `line_window`. После смены — `reindex` `force: true` |
| `identity_mismatch` / `chunking_version mismatch` | MCP `reindex` with `force: true`. Native `AUTO_INDEX_ON_START=true` — на следующем старте; shared daemon — вручную |
| `zvec_open_ok: false`, LOCK error (`Can't open lock file`, `Can't lock read-only collection`), `zvec_doc_count: 0` | Duplicate `--stdio`/`--http` per-project processes or stale zvec LOCK — restart Cursor; check `index_status.diagnostics` (`duplicate_stdio_suspected`); do not run `--http` alongside Cursor MCP on the same `INDEX_DIR`; re-run install if needed; **not** a Cyrillic-path issue — zvec stays in `INDEX_DIR/zvec/`; `reindex` with `force: true` if manifest has chunks but zvec is empty |
| `--stdio` остался после закрытия Cursor/launcher | Сервер завершится сам, если исчезнет родительская цепочка запуска (`powershell`/`cmd` или Cursor). Если закрыт только workspace, но Cursor держит launcher живым, нужен Restart MCP/IDE или `--stop-stdio-for-workspace`. Для отладки: `MCP_DISABLE_PARENT_WATCH=true` |
| After MCP binary update (zvec-go bump) | Index resets on first start; in **Native** mode with `AUTO_INDEX_ON_START=true` reindex runs automatically, else call MCP `reindex` |
| `index_owner_mismatch` | Call MCP `reindex` with `force: true`. In **Native** mode with `AUTO_INDEX_ON_START=true` this runs on next start; keep separate `INDEX_DIR` per project if you share one clone |
| zvec index dir much larger than the data (many `*.proxima` blocks) | Expected on an index built before compaction support: zvec allocates one ~5 MB block per flush. Recover with `reindex` `force: true` (recomputes embeddings, needs space for staging); an unchanged run does **not** shrink historical bloat. Manual `.proxima` deletion is not supported |
| Windows file watcher misses saves | Set `file_watcher.backend: polling` in config |
| Shared daemon: `workspace_id` required | Use `--stdio-proxy --workspace-id=<id>` or HTTP `X-Workspace-ID` |
| Shared daemon: unknown workspace | Check `daemon.yaml` id matches proxy `--workspace-id` |
| Proxy / daemon without `API_TOKEN` | `index_status` omits `workspace_root`, `index_dir`, `current_file`, `failed_files`; set `API_TOKEN` on daemon + Bearer on HTTP for full paths |
| Daemon shutdown: `503 registry is closing` | Expected while daemon stops; retry after restart or wait for in-flight requests to finish |
| Global: `collection ... is not registered on this machine` | `--project-add <root>` (или re-run install для этого проекта) |
| Global: `collection ... is registered for <other root>` | Проект скопирован или перенесён: `--project-relocate <new> --yes` для переноса, `--project-new-collection <copy>` для копии |
| Global: `--project-add` отказывает `already registered` | Копия сохранила чужой `collection_id`: используйте `--project-new-collection` |
| Global: `shared daemon unavailable` / прокси падает на каждом tool call | `--ensure-daemon`; смотреть `<userdir>/data/logs/daemon.stderr.log` |
| Global: `daemon port is already in use by another listener` | Порт `127.0.0.1:8080` занят другим приложением: освободите его или измените `http_addr` в `<userdir>/daemon.yaml` (только loopback) |
| Global: `auth.token is corrupt` | Работающий daemon держит старое значение: остановите его, удалите `<userdir>/auth.token`, затем `--ensure-daemon` |
| Global: reload отклонён с 409 (`collection in use`) | Коллекция открыта или индексируется: закройте её MCP-сессию, затем `--projects-reload` |
| Global: install отказывается заменять бинарник (`still in use`) | Закройте MCP-сессии этого проекта и повторите install; force kill не выполняется |
| Global: пустой список коллекций у проекта | Ни один проект не зарегистрирован: `--project-add <project>` |
| Global: `MCP_GLOBAL_DIR must be an absolute path` | Укажите абсолютный путь или не задавайте переменную |

## Общий daemon

**По умолчанию (global install)** общий daemon уже используется: install ставит одно приложение в user-level каталог, поднимает `127.0.0.1:8080` и настраивает в каждом проекте `--stdio-proxy --project-config <abs project.yaml>`. Прокси не открывает zvec и не создаёт `Phase1`; коллекцию выбирает `collection_id` из `project.yaml`, и агент не может переопределить её аргументами tools.

- Токен: `<userdir>/auth.token` (создаётся один раз, не регенерируется; в логи и argv не попадает).
- Singleton: `<userdir>/runtime/daemon.lock`; второй daemon не открывает коллекции.
- Новый проект: `--project-add <root>` → daemon перечитывает `projects.d` без перезапуска; чужие коллекции сохраняют handles и текущую индексацию.
- Смена **глобального** профиля (`dimensions`/`model`/chunking) → остановить и запустить daemon, затем `reindex` `force: true` для каждой коллекции.
- Смена **проектных** overrides → `--projects-reload` (409, если коллекция занята).

Legacy shared daemon (вручную, `daemon.yaml` + `--daemon-config` + `--stdio-proxy --workspace-id`) остаётся без изменений.

Подробнее: [INSTALL.md](INSTALL.md#global-layout-default), [INSTALL.md](INSTALL.md#shared-daemon-legacy-manual-mode), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/CONFIG.md](docs/CONFIG.md#daemonyaml-global-layout).

**Windows proxy (legacy):** `install.ps1 -McpMode Proxy -WorkspaceId <id> -DaemonUrl http://127.0.0.1:8080`.

## Обновление

1. Сравнить `index_status.server_version` или `<userdir>/bin/mcp-semantic-search-zvec-go --version` с [GitHub Releases](https://github.com/1CSerg/mcp-semantic-search-zvec-go/releases) или вызвать MCP `check_update` (опрашивает GitHub, кроме stub-сборки).
2. Re-run install script from updated clone/release. В global install обновляется одно приложение для всех проектов: staging → `--version` → штатная остановка daemon → backup/rollback. Если прокси ещё держит бинарник (Windows), install откажется и попросит закрыть MCP-сессию, не делая force kill.
3. Restart IDE.

Merge `config.yaml`, `-ReplaceConfig`: [INSTALL.md](INSTALL.md#update--повторный-install).

## Uninstall

Команды, `-KeepData` и удаление приложения: [INSTALL.md](INSTALL.md#uninstall).

- **Проект:** `uninstall.ps1` / `uninstall.sh` в проекте снимает регистрацию и managed-записи; индекс коллекции остаётся в user-level каталоге, другие проекты и приложение не затрагиваются.
- **Приложение:** `uninstall.ps1 -UninstallApp` / `uninstall.sh --uninstall-app`; отказывает, пока есть регистрации (`-Force` / `--force` обходит). Данные коллекций и `.env` сохраняются всегда.
