# Installation

Install into a **target project** (your codebase), not only this repository.

## Prerequisites

| Requirement | Notes |
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

On Windows, starting `mcp-semantic-search-zvec-go.exe` without flags opens the desktop GUI for status, reindex, search, and result viewing. Cursor MCP wiring still uses `run-mcp-stdio.ps1`, which passes `--stdio` explicitly. If Cursor already has the MCP process running for the same workspace, the GUI scans for a live `--stdio` process (and falls back to a verified `stdio.lock` holder via `LiveHolder`), shows a warning with that PID, and does not start its own auto-index or file watcher. Stale PIDs left in `stdio.lock` after a crashed MCP are ignored once the lock is reclaimed.

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

After moving the project to a new path, call MCP `reindex` with `force: true`, use the Windows GUI force rebuild action, or rely on `AUTO_INDEX_ON_START=true` (Native `--stdio` install only) to rebuild the index on the next MCP start. In shared daemon + `--stdio-proxy` mode, auto-index on start does not apply — call `reindex` manually. Install copies runtime libraries (`zvec_c_api.dll` / `libzvec_c_api.so` / `onnxruntime`) next to the binary from `bin/` in the clone or via fetch scripts.

**Hybrid chunking upgrade:** after updating to a server with hybrid chunking config (or switching to a `treesitter` build), run MCP `reindex` with `force: true` so `symbol_name`, `parent_scope`, `chunk_strategy`, and `chunking_version` in `index_meta.json` match the new pipeline. Shipped install binary (`-tags "zvec,onnx"`) indexes `.md`, `.markdown`, `.mdc`, `.txt` via the prose chunker; code extensions via `line_window`; AST fields on code appear only after reindex with a `treesitter` binary.

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

### Windows GUI: индексация прервана закрытием окна

Симптомы (старые версии): `indexing.state: error`, `context canceled`, zvec LOCK после повторного открытия GUI.

Fix (v0.1.8+): при следующем запуске GUI индексация **продолжается автоматически** (incremental reindex). Кнопка «Переиндексировать» остаётся для ручного запуска. Если видна «Освободить LOCK» — нажмите её или завершите лишние процессы `mcp-semantic-search-zvec-go.exe`.

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
make build-zvec    # -tags "zvec,onnx" — prose for .md/.txt; line_window for code without treesitter
```

For **AST hybrid chunking** on enabled languages (`.go`, `.py`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.ts`, `.tsx`, **1C** `.bsl`/`.os` via tree-sitter-bsl; `.dcs` SDBL query blocks when `languages.bsl.include_sdbl: true` — heuristic, not tree-sitter), build locally with `-tags "zvec,onnx,treesitter"` — not included in current Release/install builds.

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
3. Re-run install or ensure production binary includes `-tags "zvec,onnx"` and ships `onnxruntime` + `zvec` runtime libraries next to the executable.
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

## Shared daemon

One HTTP process serves multiple projects via `workspace_id`. Architecture and trade-offs: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). `daemon.yaml` reference: [docs/CONFIG.md](docs/CONFIG.md#daemonyaml-phase-5).

1. Create `daemon.yaml` (template: [templates/daemon.yaml](templates/daemon.yaml); Docker example: [templates/daemon.docker.yaml](templates/daemon.docker.yaml)).
2. Start daemon: `--daemon --daemon-config /path/to/daemon.yaml --http-addr :8080` (or `docker compose -f docker/docker-compose.daemon.yml up`).
3. Point each Cursor project MCP entry to `--stdio-proxy --workspace-id=<id> --daemon-url=http://127.0.0.1:8080`.

**Note:** `AUTO_INDEX_ON_START` from proxy install does not trigger indexing in the daemon — call MCP `reindex` per workspace after daemon start.

**Security:** set `API_TOKEN` on the daemon process when it listens beyond loopback. Without it, `index_status` / `semantic_search` via proxy omit workspace paths and file-level indexing fields (`current_file`, `failed_files`); see [docs/API.md](docs/API.md#open-daemon-redaction).

**Windows + Docker:** use install with proxy mode (launcher script in project bin):

```powershell
& "...\scripts\install\install.ps1" -TargetRoot (Get-Location).Path `
  -McpMode Proxy -WorkspaceId my-app -DaemonUrl "http://127.0.0.1:8080"
```

Per-project install (`--stdio`, default `-McpMode Native`) remains the default and does not require a daemon.
