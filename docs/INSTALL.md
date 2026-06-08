# Installation

Install into a **target project** (your codebase), not only this repository.

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| Go 1.26+ | Only for building from source |
| Git | Recommended |
| Prebuilt binary | From GitHub Release (install script) |
| ONNX Runtime + model | Required only for `local_multilingual` profile |

No Python, Docker, or uv required.

## Quick install

### Windows (PowerShell)

```powershell
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go $env:TEMP\mcp-semantic-search-zvec-go
& "$env:TEMP\mcp-semantic-search-zvec-go\scripts\install.ps1" -TargetRoot (Get-Location).Path
```

### Linux / macOS

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go /tmp/mcp-semantic-search-zvec-go
TARGET_ROOT="$PWD" bash /tmp/mcp-semantic-search-zvec-go/scripts/install.sh
```

## What install creates

```
your-project/
├── .cursor/mcp.json              # MCP wiring (merged)
├── .mcp-semantic-search-zvec-go/
│   ├── config.yaml               # settings (profiles, indexing, …)
│   ├── .env                      # secrets (created on first install)
│   ├── bin/mcp-semantic-search-zvec-go[.exe]
│   ├── install-manifest.json
│   └── data/
│       ├── index/
│       └── logs/
```

The whole `.mcp-semantic-search-zvec-go/` directory is gitignored. On first install, `.env` is copied from `templates/env.example` if it does not exist yet.

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
      "command": "${workspaceFolder}/.mcp-semantic-search-zvec-go/bin/mcp-semantic-search-zvec-go.exe",
      "args": ["--stdio"],
      "env": {
        "WORKSPACE_ROOT": "${workspaceFolder}",
        "WORKSPACE_ID": "${workspaceFolder}",
        "INDEX_DIR": "${workspaceFolder}/.mcp-semantic-search-zvec-go/data/index",
        "CONFIG_PATH": "${workspaceFolder}/.mcp-semantic-search-zvec-go/config.yaml",
        "AUTO_INDEX_ON_START": "true"
      }
    }
  }
}
```

Linux/macOS: same, but `command` without `.exe` (see `templates/cursor-mcp-linux.fragment.json`).

При старте `--stdio` бинарник сам останавливает зависшие stdio-процессы для того же workspace (`internal/lifecycle`).

**Restart Cursor** after install. Если обновляете с версии с `mcp-native.ps1` — перезапустите `install.ps1` / `install.sh` или обновите `.cursor/mcp.json` вручную.

## HTTP mode (optional)

Run alongside MCP or standalone:

```bash
.mcp-semantic-search-zvec-go/bin/mcp-semantic-search-zvec-go --http --http-addr :8080
```

Or set in systemd / Docker — see [docker/docker-compose.yml](../docker/docker-compose.yml).

## Build from source

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go
cd mcp-semantic-search-zvec-go
go build -o bin/mcp-semantic-search-zvec-go ./cmd/mcp-semantic-search-zvec-go
```

Copy binary + `config.yaml` into target `.mcp-semantic-search-zvec-go/`.

## Configure embeddings

Edit `.mcp-semantic-search-zvec-go/config.yaml` and `.env`:

1. Set `active_profile` in `config.yaml`.
2. For cloud providers: put API keys in `.env` (names from `api_key_env` in the profile).
3. For LM Studio: usually no `.env` keys; adjust `base_url`, `model`, `dimensions` in `config.yaml`.
4. Run MCP tool `reindex` after profile change.

### Offline ONNX (`local_multilingual`)

1. Set `active_profile: local_multilingual` in `config.yaml`.
2. Download model bundle into `.mcp-semantic-search-zvec-go/models/paraphrase-multilingual-MiniLM-L12-v2/` (see [CONFIG.md](CONFIG.md)).
3. Re-run install or ensure production binary includes `-tags "zvec,onnx"` and ships `onnxruntime` + `zvec` runtime libraries next to the executable.
4. Run `reindex` with `force: true`.

See [CONFIG.md](CONFIG.md).

## Verify

1. Restart Cursor.
2. Call MCP `index_status` — check `server_version`, `index_dir`.
3. After Phase 2: wait for indexing idle, then `semantic_search`.

Bootstrap (Phase 0): tools respond with stub JSON; `bootstrap: true` in status.

## Uninstall

```powershell
Remove-Item -Recurse -Force .\.mcp-semantic-search-zvec-go
# Remove semantic-search-zvec-go from .cursor/mcp.json manually until uninstall script (Phase 2)
```

## Coexistence with Python mcp-semantic-search-zvec

Both can be installed in the same project:

| | Python | Go |
|--|--------|-----|
| Install dir | `.mcp-semantic-search-zvec/` | `.mcp-semantic-search-zvec-go/` |
| MCP key | `semantic-search-zvec` | `semantic-search-zvec-go` |

Indexes are separate unless you intentionally share paths.

## Shared daemon (Phase 5)

See [ARCHITECTURE.md](ARCHITECTURE.md) — one HTTP service, multiple projects via `daemon.yaml`.
