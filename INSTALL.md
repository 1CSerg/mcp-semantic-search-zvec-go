# Installation

Two deployment layouts exist:

| Layout | What is installed | When to use |
|--------|-------------------|-------------|
| **Global (default)** | One application per user, one shared daemon, one lightweight stdio proxy per MCP session. A project stores only `project.yaml` with its `collection_id`. | Recommended. Many projects, one machine, one set of models and secrets. |
| **Legacy per-project** (`-McpMode Native` / `--mcp-mode native`) | A copy of the binary, native libraries, config, secrets and index inside each project. | Existing installs and automation that must keep the old layout. |

Both layouts are supported; the legacy one is unchanged.

## Global layout (default)

### Quick install

Windows:

```powershell
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go $env:TEMP\mcp-semantic-search-zvec-go
& "$env:TEMP\mcp-semantic-search-zvec-go\scripts\install\install.ps1" -TargetRoot (Get-Location).Path
```

Linux / macOS:

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go /tmp/mcp-semantic-search-zvec-go
bash /tmp/mcp-semantic-search-zvec-go/scripts/install/install.sh --target-root "$PWD"
```

The default install does two things, in this order:

1. **Installs or updates the application** in the user-level directory.
2. **Adds the current project** to it: `project.yaml`, the local registration and the managed IDE entries.

Installing the application and adding a project are separate operations:

```powershell
# Application only (no project); the daemon is started and left running.
& "...\scripts\install\install.ps1" -InstallAppOnly
```

```bash
bash .../scripts/install/install.sh --install-app-only
```

### User-level directory

`MCP_GLOBAL_DIR` overrides the location (it must be an absolute path). Defaults:
`%LOCALAPPDATA%\mcp-semantic-search-zvec-go` on Windows,
`$XDG_DATA_HOME/mcp-semantic-search-zvec-go` (fallback `~/.local/share/...`) elsewhere.

```text
<userdir>/
  bin/                             # one installed EXE + native libraries
  config.yaml                      # embeddings, defaults, logging (shared by all projects)
  daemon.yaml                      # listener (127.0.0.1:8080), resource caps
  .env                             # user secrets — never per project
  auth.token                       # local Bearer token for proxy ↔ daemon
  projects.d/<collection_id>.yaml  # local registry: id, root, description
  runtime/daemon.lock              # singleton daemon lock
  data/collections/<collection_id>/# manifest, meta, locks, zvec (one per project)
  data/models/                     # shared model files
  data/logs/                       # daemon/proxy/crash logs
```

### What a project contains

```text
<project>/
├── .cursor/mcp.json                                   # managed MCP entry (merged)
├── .cursor/rules/semantic-search-zvec-go.mdc          # managed agent rule
├── .roo/mcp.json                                      # managed MCP entry (merged)
├── .roo/rules/semantic-search-zvec-go.md              # managed agent rule
└── .mcp-semantic-search-zvec-go/
    └── project.yaml                                   # schema_version + collection_id
```

A project holds **no** binary, native libraries, models, index, shared config or secrets. The managed MCP entry points at the shared binary:

```json
{
  "mcpServers": {
    "semantic-search-zvec-go": {
      "command": "C:\\Users\\you\\AppData\\Local\\mcp-semantic-search-zvec-go\\bin\\mcp-semantic-search-zvec-go.exe",
      "args": ["--stdio-proxy", "--project-config", "D:\\project\\.mcp-semantic-search-zvec-go\\project.yaml"]
    }
  }
}
```

No secrets and no `INDEX_DIR`/`WORKSPACE_ROOT`: the proxy reads the project's `collection_id`, ensures the daemon is running and passes the id on every request. The absolute paths in this IDE JSON are fine; the portable `project.yaml` never contains one.

### project.yaml

Minimal file:

```yaml
schema_version: 1
collection_id: 9ab86bc3-48b1-4fc9-8c3c-3f8ab01bbcd0
```

Allowed overrides (everything else belongs to the shared config and is rejected):

```yaml
schema_version: 1
collection_id: 9ab86bc3-48b1-4fc9-8c3c-3f8ab01bbcd0
description: "Order service"
active_profile: local_multilingual     # must exist in the shared config.yaml
indexing:
  extensions: [".go", ".md"]
  skip_dirs: [".git", "vendor", ".mcp-semantic-search-zvec-go"]
  max_file_bytes: 2097152
  chunking:
    min_chunk_tokens: 24
    prose_overlap_ratio: 0.12
    languages:
      bsl:
        enabled: false                 # only the fields you set are overridden
file_watcher:
  enabled: true
```

`schema_version` may be omitted for older files (treated as 1). Unknown keys, another version, and `index_dir`-style path keys are rejected.

### Project commands

All commands are subcommands of the same installed binary; the install scripts call them for you.

```powershell
$exe = "$env:LOCALAPPDATA\mcp-semantic-search-zvec-go\bin\mcp-semantic-search-zvec-go.exe"
& $exe --project-add D:\project            # register, ensure the daemon, reload
& $exe --project-remove D:\project         # unregister (collection data is kept)
& $exe --project-new-collection D:\copy    # give a copied project its own collection
& $exe --project-relocate D:\moved --yes   # move a project, keeping its collection
& $exe --projects-reload                   # re-read projects.d without restarting
& $exe --ensure-daemon                     # start the daemon if it is not running
& $exe --stop-daemon                       # graceful shutdown
```

Rules:

- A new root gets a new UUID v4 collection id.
- The same root with the same id is idempotent.
- The same root with a **different** id is refused: run `--project-remove` first.
- The same id on a **different** root is refused: use `--project-new-collection` for a copy.
- `--project-relocate` keeps the id, refuses while the collection is open in the daemon, and needs `--yes`.
- A management command that the daemon refuses (409, the collection is in use) is **rolled back** and exits non-zero: the registry on disk never disagrees with what the daemon serves.
- `--project-new-collection` changes `project.yaml` and the registration only. A copied project's `.cursor/mcp.json` and `.roo/mcp.json` still point at the **original** absolute `--project-config`, so re-run install for the copy (`install.ps1 -TargetRoot <copy>` / `install.sh --target-root <copy>`) to rewrite them before using it.

Adding a project does not restart anything: the running daemon re-reads `projects.d` and serves the new collection immediately. Other collections keep their open handles and their running indexing jobs.

### Daemon and security

- The user-level daemon listens on **`127.0.0.1:8080`** only. A non-loopback `http_addr` is rejected for this mode; use the legacy explicit `--daemon-config` if you really need LAN access.
- `auth.token` is 32 random bytes (hex), created once with exclusive access and never regenerated. Proxies read it from the user directory; it is never printed or put on a command line.
- One daemon per user is enforced by `runtime/daemon.lock`; a second daemon exits without touching any collection.
- The daemon keeps running after the IDE closes. Closing an MCP session ends only its proxy.

### Auto-index, LRU and identity changes

- `auto_index_on_open: true` (default in the generated `daemon.yaml`) builds or refreshes a collection the first time it is opened. One open produces at most one startup job.
- `max_open_workspaces` (default 10) bounds open collections. A collection that is indexing is never evicted; when every slot is busy the request fails with a diagnosable 503.
- A **global** profile change (`dimensions`, `model`, `base_url`, chunking strategy/version) requires a daemon restart and then `reindex` with `force: true` for each collection. `index_status` reports `identity_mismatch` until then, and search refuses to use the new model against old vectors.
- A **project** change (`project.yaml` overrides) is applied by `--projects-reload`; the changed collection is reopened, and a reload is refused with 409 while that collection is in use.

### Migration from a project-local install

Detection is explicit. Without `-MigrateLegacy` / `--migrate-legacy`, adding a project that still holds `.mcp-semantic-search-zvec-go/bin/mcp-semantic-search-zvec-go.exe` stops with instructions instead of creating a second owner of the index.

```powershell
& "...\scripts\install\install.ps1" -TargetRoot (Get-Location).Path -MigrateLegacy
```

```bash
bash .../scripts/install/install.sh --target-root "$PWD" --migrate-legacy
```

Migration:

1. Installs the shared application **without deleting** the old install.
2. Stops the old per-project MCP processes with `--stop-stdio-for-workspace` (never a blanket kill).
3. Merges profiles into the shared `config.yaml`. A profile name that already exists with a **different** model/dimensions/endpoint is a conflict: the script stops and asks you to rename it (no "last one wins").
4. Copies missing secret **names** into the shared `.env`; values are not printed.
5. Registers the project and rewrites only the managed MCP entry and rules.
6. Leaves the old `.mcp-semantic-search-zvec-go/` tree as a backup until you delete it.
7. The new index is built from scratch under `data/collections/<id>/`; the old index files are never copied or edited.

### Uninstall

Remove **one project** (other projects, their data and the application are untouched):

```powershell
& .\.mcp-semantic-search-zvec-go\uninstall.ps1
```

```bash
./.mcp-semantic-search-zvec-go/uninstall.sh
```

This unregisters the project, removes the managed MCP entries and rules, and deletes the project's `project.yaml`. The collection's index stays in the user directory.

Remove the **application** (refused while projects are registered unless forced):

```powershell
& "$env:LOCALAPPDATA\mcp-semantic-search-zvec-go\uninstall.ps1" -UninstallApp
& "$env:LOCALAPPDATA\mcp-semantic-search-zvec-go\uninstall.ps1" -UninstallApp -Force
```

```bash
bash "$XDG_DATA_HOME/mcp-semantic-search-zvec-go/uninstall.sh" --uninstall-app [--force]
```

Collection data and `.env` are preserved in every branch, including `-KeepData`.

### Update

Re-run the install command from an updated clone or release. The application binary is replaced with staging, backup and rollback:

1. The new binary is verified with `--version`.
2. The daemon is asked to stop through the admin API (never a force kill).
3. If a proxy still holds the binary (Windows cannot replace a mapped EXE), the install **refuses** and tells you to close the IDE MCP session; the working version is left in place.
4. On any failure the previous binary is restored.

`config.yaml` is merged (your `active_profile`, profiles, lists and comments are preserved); `daemon.yaml`, `.env`, `auth.token` and every collection are left alone.

### Управление без GUI

Приложение headless: запуск без аргументов печатает справку и завершается с кодом 0. Всё, что раньше делалось в окне, делается командами и MCP tools:

| Задача | Команда / tool |
|--------|----------------|
| Список зарегистрированных коллекций | `--projects-reload` (перечитать `projects.d`) и `index_status` через proxy любого проекта; регистрации лежат в `<userdir>/projects.d/` |
| Добавить проект в общее приложение | `--project-add <root>` |
| Снять регистрацию, сохранив данные | `--project-remove <root>` |
| Статус индексации и прогресс | MCP `index_status` (или `GET /v1/status?workspace_id=<id>`) |
| Полная переиндексация («force rebuild») | MCP `reindex` с `force: true` (или `POST /v1/reindex` с `{"force": true}`) |
| Поиск | MCP `semantic_search` (или `POST /v1/search`) |
| Запустить / остановить daemon после перезагрузки | `--ensure-daemon` / `--stop-daemon` |
| Проверить обновления | MCP `check_update` |

Глобальный профиль эмбеддингов меняется только при остановленном daemon: `--stop-daemon`, правка `<userdir>/config.yaml`, затем `--ensure-daemon` и `reindex` с `force: true` для каждой коллекции.

## Legacy per-project layout

Pass `-McpMode Native` (PowerShell) or `--mcp-mode native` (Bash) to keep the original behaviour described below. Everything from "What install creates" onward in this document describes that layout.

Install into a **target project** (your codebase), not only this repository.

## Prerequisites
|-------------|-------|
| Go 1.26+ | Only for building from source |
| Git | Recommended |
| Prebuilt binary | From GitHub Release (install script) |
| ONNX Runtime + model | Required only for `local_multilingual` profile |
| Python 3 + `ruamel.yaml` | Optional on Windows; recommended for **update** merge of `config.yaml` (Linux install already uses `python3` for `.cursor/mcp.json`) |

No Docker or uv required.

## Quick install

### Windows (PowerShell)

```powershell
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go $env:TEMP\mcp-semantic-search-zvec-go
& "$env:TEMP\mcp-semantic-search-zvec-go\scripts\install\install.ps1" -TargetRoot (Get-Location).Path
```

### Linux / macOS

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go /tmp/mcp-semantic-search-zvec-go
TARGET_ROOT="$PWD" bash /tmp/mcp-semantic-search-zvec-go/scripts/install/install.sh
```

## What install creates

```
your-project/
├── .cursor/mcp.json              # MCP wiring (merged)
├── .cursor/rules/semantic-search-zvec-go.mdc  # agent rule (install-managed, English)
├── .roo/mcp.json                 # Roo/Zoo Code MCP wiring (merged)
├── .roo/rules/semantic-search-zvec-go.md      # Roo agent rule (install-managed)
├── .mcp-semantic-search-zvec-go/
│   ├── config.yaml               # settings (profiles, indexing, …)
│   ├── .env                      # secrets (created on first install)
│   ├── bin/
│   │   ├── mcp-semantic-search-zvec-go.exe
│   │   ├── run-mcp-stdio.ps1     # Cursor launcher (Windows)
│   │   ├── run-mcp-proxy.ps1     # Cursor launcher (proxy mode)
│   │   └── *.dll                 # runtime libs (Windows)
│   ├── install-manifest.json
│   ├── uninstall.ps1             # Windows uninstall (also uninstall.sh on Linux/macOS)
│   ├── logs/                     # server.log (created at runtime)
│   └── data/
│       └── index/
```

The whole `.mcp-semantic-search-zvec-go/` directory is gitignored. On first install, `.env` is copied from `templates/env.example` if it does not exist yet.

On Windows, starting `mcp-semantic-search-zvec-go.exe` without flags prints the help text and exits `0`; pass an explicit mode (`--stdio` for MCP, `--http` for the REST API). Cursor MCP wiring uses `run-mcp-stdio.ps1`, which passes `--stdio` explicitly. If Cursor already has the MCP process running for the same workspace, `--stdio` reclaims orphan locks, reports the competing process through `index_status` (`diagnostics.duplicate_stdio_suspected` / a live `stdio.lock` holder), and does not compete for the index; stale PIDs in `stdio.lock` are ignored once the lock is reclaimed. To stop a stale instance use `--stop-stdio-for-workspace <workspace>`.

## Zoo Code / Roo Code

Install configures **both** Cursor and Roo/Zoo Code in the same target project:

| IDE | MCP config | Agent rule |
|-----|------------|------------|
| Cursor | `.cursor/mcp.json` | `.cursor/rules/semantic-search-zvec-go.mdc` |
| Roo / Zoo Code | `.roo/mcp.json` | `.roo/rules/semantic-search-zvec-go.md` |

The MCP server entry (`semantic-search-zvec-go`) and launcher/env match Cursor install. After install or update, **restart Roo Code** (MCP has no hot-reload). Uninstall removes only install-managed Roo files (marker `managedBy: mcp-semantic-search-zvec-go` in the rule).

## Update / повторный install

Re-run the same install command from an updated clone or release. The script:

- Updates the binary and runtime DLLs/so/dylib
- **Overwrites** `.cursor/rules/semantic-search-zvec-go.mdc` and `.roo/rules/semantic-search-zvec-go.md` with the latest agent rule templates
- **Merges** new keys from repo `config.yaml` into your `.mcp-semantic-search-zvec-go/config.yaml` (your `active_profile`, profiles, lists, and comments are preserved), including `indexing.chunking` and provider-aware `max_input_tokens` (`onnx` → 256, `openai_compatible` → 512)
- Prints: `WARNING: Chunking strategy updated. You must run MCP 'reindex' with force: true after starting the IDE.`
- Does **not** overwrite `.env` or `data/index/`

For config merge, install `ruamel.yaml` once from the clone:

```bash
pip install -r scripts/install/requirements.txt
```

Force full config reset (discard local YAML settings):

```powershell
& "...\scripts\install\install.ps1" -TargetRoot (Get-Location).Path -ReplaceConfig
```

```bash
REPLACE_CONFIG=1 TARGET_ROOT="$PWD" bash .../scripts/install/install.sh
```

If merge dependencies are missing and `config.yaml` already exists, install **preserves** your file and prints a warning.

After moving the project to a new path, call MCP `reindex` with `force: true`, or rely on `AUTO_INDEX_ON_START=true` (Native `--stdio` install only) to rebuild the index on the next MCP start. In shared daemon + `--stdio-proxy` mode, auto-index on start does not apply — call `reindex` manually. Install copies runtime libraries (`zvec_c_api.dll` / `libzvec_c_api.so` / `onnxruntime`) next to the binary from `bin/` in the clone or via fetch scripts.

**Hybrid chunking upgrade:** after updating to a server with hybrid chunking config (or upgrading from a legacy binary without `treesitter`), run MCP `reindex` with `force: true` so `symbol_name`, `parent_scope`, `chunk_strategy`, and `chunking_version` in `index_meta.json` match the new pipeline. Shipped install binary (`-tags "zvec,onnx,treesitter"`) indexes `.md`, `.markdown`, `.mdc`, `.txt` via the prose chunker and enabled code extensions via AST cAST; legacy `-tags "zvec,onnx"` builds fall back to `line_window` for code.

## Configure secrets

1. Open `.mcp-semantic-search-zvec-go/.env`.
2. Fill keys for your `active_profile` in `config.yaml` (e.g. `ROUTERAI_API_KEY`, `DASHSCOPE_API_KEY`).
3. You may add custom `KEY=VALUE` pairs and reference them via `api_key_env` in a profile.

Local LM Studio usually needs no keys.

## Cursor MCP wiring

Entry added to `.cursor/mcp.json`:

Windows (`.cursor/mcp.json` after `install.ps1`):

```json
{
  "mcpServers": {
    "semantic-search-zvec-go": {
      "command": "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe",
      "args": [
        "-NoProfile",
        "-ExecutionPolicy",
        "Bypass",
        "-File",
        "D:\\project\\.mcp-semantic-search-zvec-go\\bin\\run-mcp-stdio.ps1"
      ],
      "env": {
        "WORKSPACE_ROOT": "D:\\project",
        "WORKSPACE_ID": "D:\\project",
        "INDEX_DIR": "D:\\project\\.mcp-semantic-search-zvec-go\\data\\index",
        "CONFIG_PATH": "D:\\project\\.mcp-semantic-search-zvec-go\\config.yaml",
        "AUTO_INDEX_ON_START": "true"
      }
    }
  }
}
```

Install копирует exe, DLL и launcher-скрипты в `.mcp-semantic-search-zvec-go/bin/` проекта. Cursor вызывает `powershell.exe` (ASCII-путь) с `-File` на `run-mcp-stdio.ps1`; скрипт переходит в `bin/` и запускает локальный exe. Пути workspace/index/config задаются явно в `env` (абсолютные, backslash-only) — не через `${workspaceFolder}` и не через `%LOCALAPPDATA%`.

Это обходит баг Cursor на Windows: прямой spawn exe по путям с пробелами/Unicode через `${workspaceFolder}` часто падает, хотя ручной запуск работает.

Linux/macOS: `${workspaceFolder}` + бинарник в проекте (see `templates/cursor-mcp-linux.fragment.json`). Windows template: `templates/cursor-mcp-windows.fragment.json`.

### Cursor MCP error на Windows

1. **Проверка:** из PowerShell в каталоге проекта:
   ```powershell
   .\.mcp-semantic-search-zvec-go\bin\mcp-semantic-search-zvec-go.exe --version
   powershell -NoProfile -ExecutionPolicy Bypass -File .\.mcp-semantic-search-zvec-go\bin\run-mcp-stdio.ps1
   ```
   (вторую команду прервите Ctrl+C после старта)
2. **Лог Cursor:** `%APPDATA%\Cursor\logs\<session>\mcpprocess.log` — искать `spawn ... ENOENT`, `non-retryable`.
3. **Fix ladder (по порядку):**
   - Re-run `install.ps1 -TargetRoot (Get-Location).Path`
   - Restart MCP в Settings
   - Kill зависший `McpProcess` (PID из `mcpprocess.log`) + полный restart Cursor
   - Сменить ключ сервера в `.cursor/mcp.json` (сброс non-retryable snapshot)
   - Developer: Reload Window
4. **После переноса проекта** — обязательно re-run install (обновляет `mcp.json` env и launcher paths).
5. **Fallback:** если PowerShell не стартует — вручную переключите `command` на `C:\\Windows\\System32\\cmd.exe`, `args` на `["/c", "D:\\...\\bin\\run-mcp-stdio.cmd"]` (шаблон `templates/bin/run-mcp-stdio.cmd`).

### Semantic search: zvec LOCK / duplicate MCP

Симптомы: `index_status` → `zvec_open_ok: false`, `zvec_error` содержит `Can't open lock file`, `zvec_doc_count: 0` при ненулевом `indexed_chunks_manifest`; `semantic_search` возвращает пустой `results`.

Причина: два процесса `mcp-semantic-search-zvec-go --stdio` на один workspace (часто после restart Cursor без завершения старого MCP).

В `--stdio` и `--stdio-proxy` сервер следит за цепочкой запуска (`powershell`/`cmd` и Cursor) и завершает себя, когда один из этих процессов исчезает. Это не срабатывает при обычном закрытии workspace, если Cursor продолжает держать MCP launcher живым. Для ручной отладки watchdog можно отключить переменной `MCP_DISABLE_PARENT_WATCH=true`.

Fix:

1. Полный restart Cursor.
2. Проверить один процесс: `tasklist /FI "IMAGENAME eq mcp-semantic-search-zvec-go.exe"`.
3. Re-run `install.ps1 -TargetRoot` (обновляет `workspace-root.txt` и `mcp.json`).
4. `index_status` → при `diagnostics.duplicate_stdio_suspected` следовать `diagnostics.hint`.
5. При необходимости MCP `reindex` с `force: true`.

### Индексация прервана остановкой процесса

Симптомы (старые версии): `indexing.state: error`, `context canceled`, zvec LOCK после повторного открытия индекса.

Fix (v0.1.8+): при следующем старте `--stdio` / `--http` / daemon прерванная индексация **продолжается автоматически** (incremental reindex; `manifest` пропускает уже проиндексированные файлы). Для ручного запуска — MCP `reindex` (полный — с `force: true`). Если в `index_status.diagnostics` есть zvec LOCK или `duplicate_stdio_suspected` — завершите лишние процессы `mcp-semantic-search-zvec-go.exe` (`--stop-stdio-for-workspace <workspace>`) и повторите.

### Индексация: частичные ошибки файлов

Симптомы: `index_status` → `indexing.state: idle`, `indexing.files_failed > 0` (часто zvec `File is too small` на отдельном файле).

Это **не** fatal: остальные файлы проиндексированы, `semantic_search` работает по ним. Пропущенные пути — в `indexing.failed_files` (до 20); полный текст ошибок — в `diagnostics.log_file` (`index file skipped`).

Fix:

1. `index_status` → `indexing.failed_files` и `diagnostics.log_file`.
2. Tail `diagnostics.log_file` — строки `index file skipped`.
2. MCP `reindex` с `force: true` для проблемных путей или всего индекса.
3. Отличие от fatal: при `indexing.state: error` job прерван (embed down, `index_owner_mismatch`, stall) — см. таблицу troubleshooting в [AGENTS.md](AGENTS.md).

### Несколько репозиториев (два окна Cursor)

| Режим | Multi-repo | Примечание |
|-------|------------|------------|
| Native `--stdio` + Windows project-local | OK | exe + launcher в `.mcp-semantic-search-zvec-go/bin/` на каждый проект |
| Linux/macOS native install | OK | `${workspaceFolder}` в `.cursor/mcp.json`, бинарник в проекте |
| Multi-root workspace (одно окно, несколько корней) | Нет | Нужен shared daemon + `--stdio-proxy` |
| Docker [`docker-compose.yml`](docker/docker-compose.yml) | Нет | Один workspace, один контейнер |
| Docker [`docker-compose.daemon.yml`](docker/docker-compose.daemon.yml) | OK | Shared daemon + proxy в Cursor |

Smoke: `scripts/smoke/run-mcp-staging-multi-windows.ps1` (project-local bin, два проекта, проверка `mcp.json`).

### Docker: один проект vs несколько

**Per-project** ([`docker/docker-compose.yml`](docker/docker-compose.yml)) — один bind-mount `/workspace`, режим `--http`. Подходит для одного репозитория.

**Shared daemon** ([`docker/docker-compose.daemon.yml`](docker/docker-compose.daemon.yml)) — несколько `workspaces[]` в `daemon.yaml`, один контейнер `--daemon`. Пример конфигурации: [templates/daemon.docker.yaml](templates/daemon.docker.yaml).

```powershell
# Пример: два репо на хосте
$env:WS_ALPHA_ROOT = "D:\projects\repoA"
$env:WS_ALPHA_INDEX = "D:\projects\repoA\.mcp-semantic-search-zvec-go\data\index"
$env:WS_BETA_ROOT = "D:\projects\repoB"
$env:WS_BETA_INDEX = "D:\projects\repoB\.mcp-semantic-search-zvec-go\data\index"
$env:DAEMON_CONFIG_PATH = "D:\path\to\daemon.yaml"   # пути внутри контейнера — см. template
docker compose -f docker/docker-compose.daemon.yml up --build
```

Cursor в каждом проекте (Windows, proxy через launcher):

```powershell
& "...\scripts\install\install.ps1" -TargetRoot (Get-Location).Path `
  -McpMode Proxy -WorkspaceId ws-alpha -DaemonUrl "http://127.0.0.1:8080"
```

`.cursor/mcp.json` получит `command` → `powershell.exe`, `-File` → `run-mcp-proxy.ps1`, `args` → `--stdio-proxy --workspace-id ...`.

**Bind-mount и Unicode:** пути с кириллицей на Windows поддерживаются для `INDEX_DIR` начиная с v0.1.5 (zvec-go: ACP-кодирование путей; MCP: отключение mmap на non-ASCII путях). На Google Drive возможны единичные `files_failed` — смотрите `server.log`.

При старте `--stdio` бинарник сам останавливает зависшие stdio-процессы для того же workspace (`internal/lifecycle`).

**Restart Cursor** after install.

## HTTP mode (optional)

Run alongside MCP or standalone:

```bash
.mcp-semantic-search-zvec-go/bin/mcp-semantic-search-zvec-go --http
```

Default bind is `127.0.0.1:8080` (loopback). For LAN access set `server.http_addr: ":8080"` in config or `HTTP_ADDR=:8080`.

Or set in systemd / Docker — see [docker/docker-compose.yml](docker/docker-compose.yml) (one project; explicit `:8080` bind) or [docker/docker-compose.daemon.yml](docker/docker-compose.daemon.yml) (multi-repo daemon).

## Build from source

Plain `go build` without tags produces a **stub** binary (no zvec/ONNX — semantic search will not work). For a production binary matching **GitHub Release and install scripts**:

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go
cd mcp-semantic-search-zvec-go
make fetch-zvec-libs
make build-zvec    # -tags "zvec,onnx,treesitter" — same as GitHub Release and install scripts
```

Shipped Release/install builds include full hybrid AST for enabled languages (`.go`, `.py`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.ts`, `.tsx`, **1C** `.bsl`/`.os` via tree-sitter-bsl; `.dcs` SDBL query blocks when `languages.bsl.include_sdbl: true` — heuristic, not tree-sitter). Legacy fallback without AST: `-tags "zvec,onnx"` or `zvec,!treesitter` (prose for `.md`/`.txt`; `line_window` for code).

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for Windows CGO, ONNX runtime, tree-sitter, and cross-compile notes.

Copy binary, runtime libs, and `config.yaml` into target `.mcp-semantic-search-zvec-go/`.

## Configure embeddings

Edit `.mcp-semantic-search-zvec-go/config.yaml` and `.env`:

1. Set `active_profile` in `config.yaml`.
2. For cloud providers: put API keys in `.env` (names from `api_key_env` in the profile).
3. For LM Studio: usually no `.env` keys; adjust `base_url`, `model`, `dimensions` in `config.yaml`.
4. Run MCP tool `reindex` after profile change.

### Offline ONNX (`local_multilingual`)

1. Set `active_profile: local_multilingual` in `config.yaml`.
2. Download model bundle into `.mcp-semantic-search-zvec-go/models/paraphrase-multilingual-MiniLM-L12-v2/` (see [docs/CONFIG.md](docs/CONFIG.md)).
3. Re-run install or ensure production binary includes `-tags "zvec,onnx,treesitter"` and ships `onnxruntime` + `zvec` runtime libraries next to the executable.
4. Run `reindex` with `force: true`.

See [docs/CONFIG.md](docs/CONFIG.md).

## Verify

1. Restart Cursor.
2. Call MCP `index_status` — check `server_version`, `index_dir`.
3. After indexing idle, call `semantic_search`.

## Uninstall

Close **Cursor** (or disable the `semantic-search-zvec-go` MCP server in settings) before uninstall so `.cursor/mcp.json` and lock files are not held open. If uninstall reports errors while the IDE is running, close Cursor and run the script again.

From the **project root**:

```powershell
& .\.mcp-semantic-search-zvec-go\uninstall.ps1
# Keep index and models:
& .\.mcp-semantic-search-zvec-go\uninstall.ps1 -KeepData
```

```bash
./.mcp-semantic-search-zvec-go/uninstall.sh
KEEP_DATA=1 ./.mcp-semantic-search-zvec-go/uninstall.sh
```

Uninstall removes the MCP entry from `.cursor/mcp.json`, the install-managed Cursor rule `.cursor/rules/semantic-search-zvec-go.mdc` (only if marked `managedBy: mcp-semantic-search-zvec-go`), and the `.mcp-semantic-search-zvec-go/` tree (unless `-KeepData` / `KEEP_DATA=1`). Other files in `.cursor/rules/` are left intact.

Install copies `uninstall.ps1` / `uninstall.sh` into `.mcp-semantic-search-zvec-go/`; re-run install to refresh bundled scripts after an upgrade.

## Coexistence with Python mcp-semantic-search-zvec

Both can be installed in the same project:

| | Python | Go |
|--|--------|-----|
| Install dir | `.mcp-semantic-search-zvec/` | `.mcp-semantic-search-zvec-go/` |
| MCP key | `semantic-search-zvec` | `semantic-search-zvec-go` |

Indexes are separate unless you intentionally share paths.

## Shared daemon (legacy manual mode)

One HTTP process serves multiple projects via `workspace_id`. Architecture and trade-offs: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). `daemon.yaml` reference: [docs/CONFIG.md](docs/CONFIG.md#daemonyaml-phase-5).

> **Note:** the global layout above already does this, automatically and with a local token. This section describes the older manual setup: an explicit `daemon.yaml`, an explicit `--daemon-config`, and `--stdio-proxy --workspace-id`. It remains supported and unchanged.

1. Create `daemon.yaml` (template: [templates/daemon.yaml](templates/daemon.yaml); Docker example: [templates/daemon.docker.yaml](templates/daemon.docker.yaml)).
2. Start daemon: `--daemon --daemon-config /path/to/daemon.yaml --http-addr :8080` (or `docker compose -f docker/docker-compose.daemon.yml up`).
3. Point each Cursor project MCP entry to `--stdio-proxy --workspace-id=<id> --daemon-url=http://127.0.0.1:8080`.

**Note:** `AUTO_INDEX_ON_START` from proxy install does not trigger indexing in the daemon — call MCP `reindex` per workspace after daemon start.

**Security:** set `API_TOKEN` on the daemon process when it listens beyond loopback — the daemon **refuses to start** on non-loopback addresses without it. Without a token on loopback, `index_status` / `semantic_search` via proxy omit workspace paths and file-level indexing fields (`current_file`, `failed_files`); see [docs/API.md](docs/API.md#open-daemon-redaction).

**Windows + Docker:** use install with proxy mode (launcher script in project bin):

```powershell
& "...\scripts\install\install.ps1" -TargetRoot (Get-Location).Path `
  -McpMode Proxy -WorkspaceId my-app -DaemonUrl "http://127.0.0.1:8080"
```

Per-project install (`--stdio`, `-McpMode Native`) remains available and does not require a daemon.

## Troubleshooting the global layout

| Symptom | Action |
|---------|--------|
| `collection_id is not registered on this machine` | Run `--project-add <project>` (or re-run install for that project). |
| `collection … is registered for <other root>` | The project was copied or moved. `--project-relocate <new> --yes` for a move, `--project-new-collection <copy>` for a copy. |
| `--project-add` refuses with `already registered` | The copied `project.yaml` still carries the original id. Use `--project-new-collection`. |
| `shared daemon unavailable` / proxy fails on every tool call | Start it: `--ensure-daemon`. Check `data/logs/daemon.stderr.log` under the user dir. |
| `daemon port is already in use by another listener` | Something else owns `127.0.0.1:8080`. Stop it or change `http_addr` in `<userdir>/daemon.yaml` (loopback only). |
| `auth.token is corrupt` | A running daemon still holds the old value. Stop it, delete `<userdir>/auth.token`, then `--ensure-daemon`. |
| `daemon config: …` after editing `daemon.yaml` | The daemon validates strictly on start; fix the reported key. An empty registry is valid. |
| `identity_mismatch` after changing the shared profile | Restart the daemon, then `reindex` with `force: true` for each collection. |
| reload refused with 409 (`collection in use`) | A collection being changed is open or indexing. Close that project's MCP session, then `--projects-reload`. |
| Install refuses to replace the binary (`still in use`) | Close the IDE MCP sessions for the affected project and re-run install; no force kill is performed. |
| No collection is listed for a project | No project is registered yet: `--project-add <project>`. |
| `MCP_GLOBAL_DIR must be an absolute path` | Use an absolute path (or unset it to use the platform default). |
