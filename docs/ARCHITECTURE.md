# Architecture

## Overview

```mermaid
flowchart TB
  subgraph clients [Clients]
    Cursor[Cursor / Roo MCP]
    OtherSW[Other software]
  end

  subgraph process [mcp-semantic-search-zvec-go]
    MCP[MCP stdio]
    HTTP[HTTP REST]
    SVC[Service layer]
    Idx[Indexer]
    Emb[Embeddings]
    Zvec[zvec collection]
    Man[SQLite manifest]
    Watch[File watcher]
  end

  WS[Workspace files RO]
  Data[.mcp-semantic-search-zvec-go/data]

  Cursor --> MCP
  OtherSW --> HTTP
  MCP --> SVC
  HTTP --> SVC
  SVC --> Idx
  Idx --> Emb
  Idx --> Man
  Emb --> Zvec
  WS --> Idx
  Watch --> WS
  Zvec --> Data
  Man --> Data
```

## Design principles

1. **Single service layer** — `internal/service` shared by MCP and HTTP; identical JSON semantics.
2. **Embedded vector store** — zvec in-process; no external DB server.
3. **Resilience first** — graceful shutdown, lock recovery, health/readiness probes.
4. **Two deployment modes** — per-project (default) and shared daemon.

## Deployment modes

### Mode A — Per-project (default)

One OS process bound to one `WORKSPACE_ROOT`. Each target project has `.mcp-semantic-search-zvec-go/` with its own index.

- Full isolation of locks, watchers, crashes.
- Cursor: one MCP entry per project in `.cursor/mcp.json`.
- Trade-off: N projects → N processes (N × ONNX memory in local mode).

### Mode B — Shared daemon

One long-running HTTP server serves multiple workspaces via `workspace_id`.

- `daemon.yaml` registers workspaces: `id`, `root`, `index_dir`, `config_path`.
- HTTP: header `X-Workspace-ID` or JSON/query field `workspace_id`.
- `internal/daemon.WorkspaceRegistry` with LRU eviction (`max_open_workspaces`).
- MCP: thin `--stdio-proxy --workspace-id=... --daemon-url=...` for Cursor.
- CLI: `--daemon --daemon-config daemon.yaml` starts HTTP only; no per-project stdio cleanup.

### Registry lifecycle

`internal/daemon.WorkspaceRegistry` lazy-opens workspaces on first request (cold-open). Concurrent opens for the same `workspace_id` share one two-phase init (`config` → `phase1` service). `BorrowService` tracks in-flight HTTP handlers.

When `max_open_workspaces` is exceeded, LRU evicts idle handles from the map, then **`discardHandle` runs asynchronously** (cancel workspace ctx → `Phase1.Close()`). A `discards` counter tracks in-flight evictions.

`Close()` sets `closing`, cancels the registry context, drains borrows, cold-opens, and **async evictions** (`discards`) (default 30 s), then closes remaining idle handles and shuts down the zvec runtime. While closing, new `BorrowService` calls return `ErrRegistryClosing` → HTTP **503** `registry is closing`.

## Package layout

| Package | Role |
|---------|------|
| `cmd/mcp-semantic-search-zvec-go` | CLI entry, signal handling |
| `internal/transport/mcp` | MCP stdio (go-sdk) |
| `internal/transport/http` | REST v1, `/health`, `/ready` |
| `internal/service` | Core API |
| `internal/indexer` | Scan, chunk, background jobs |
| `internal/indexer/chunk` | `ChunkRouter`, `line_window`, `ProcessBatches`; `prose/`; `ast/` (build tag `treesitter`) |
| `internal/embeddings` | `openai_compatible`, `onnx` |
| `internal/store/zvec` | zvec-go wrapper |
| `internal/store/manifest` | SQLite per-file manifest |
| `internal/lock` | Cross-process `index.lock` |
| `internal/watcher` | fsnotify + polling |
| `internal/config` | YAML + env |
| `internal/daemon` | Multi-tenant registry |

## Data layout

```
.mcp-semantic-search-zvec-go/
├── config.yaml
├── bin/mcp-semantic-search-zvec-go[.exe]
├── models/                    # ONNX model bundles
├── logs/
│   ├── server.log
│   └── mcp.log
└── data/
    ├── index/
    │   ├── manifest.db
    │   ├── index_meta.json    # workspace owner fingerprint
    │   ├── index.lock
    │   ├── stdio.lock
    │   └── zvec/ws_<hash>/
```

### index_meta.json

Binds index to one workspace via `WORKSPACE_ID` / `workspace_fingerprint`. Mismatch from another project blocks writes to prevent manifest/zvec mixing.

### index.lock

- OS advisory exclusive lock (`flock` on Unix, `LockFileEx` on Windows) on an open fd for the holder's lifetime.
- Content: `PID` + start time + heartbeat (diagnostics, `HolderPID` / `LiveHolder`); format unchanged from earlier versions.
- `LiveHolder`: returns PID only when that process is alive and matches the identity recorded in the lock file (guards against stale or reused PIDs).
- `TryAcquire`: reclaims orphaned lock files first, then open `O_CREATE|O_RDWR`, non-blocking exclusive lock; no `O_EXCL` / TOCTOU reclaim.
- `ReclaimStale`: open + non-blocking lock; if acquired, file is orphaned (holder crashed) → remove.
- `lock_stale_seconds` / `isStale()` retained for maintenance diagnostics only (uninstall, legacy files).
- `heartbeat_seconds` is deprecated for mutual exclusion; indexing no longer calls `Heartbeat`.

### stdio.lock

- Acquired at `--stdio` startup in `PrepareStdio` (per workspace `INDEX_DIR`).
- Ensures only one native MCP stdio process serves a project index at a time.
- Before acquire: `stopStaleStdioInstances` kills prior `--stdio` processes matched by workspace path (case-insensitive on Windows) or `bin/workspace-root.txt`; orphaned `stdio.lock` files are reclaimed when the OS lock is released.
- Lock diagnostics use `LiveHolder` (alive PID + start-time match), not raw `HolderPID` alone.
- Windows GUI detects a competing MCP via `FindStdioForWorkspace` (process scan for `--stdio` + workspace), then `LiveHolder` as fallback; stale lock payloads are not shown to the user.
- If acquire fails after retries, process exits with a clear stderr hint (Cursor may show MCP error briefly — preferred over broken search).
- Released on normal shutdown.

## Hybrid chunking pipeline

When `indexing.chunking.strategy` is `hybrid` (default), file splitting goes through `ChunkRouter` instead of legacy slideWindow-only paths.

```mermaid
flowchart LR
  File[File bytes] --> Router[ChunkRouter]
  Router -->|".md .markdown .mdc .txt" + hybrid/prose strategy| Prose[prose chunker]
  Router -->|".go .py .js .jsx .mjs .cjs .ts .tsx .bsl .os" + lang enabled + treesitter build| AST[cAST / tree-sitter]
  Router -->|".dcs" + bsl.include_sdbl| DCS[DCS query regex extract]
  DCS --> SDBL[heuristic SDBL chunker]
  Router -->|other ext or AST unavailable| LW[line_window]
  AST -->|embedded BSL query strings + include_sdbl| SDBL
  AST -->|oversized leaf / parse errors| LW
  SDBL -->|oversized query| LW
  Prose -->|oversized segment| LW
  Router --> Emit[emit callback]
  LW --> Emit
  SDBL --> Emit
  Prose --> Emit
  Emit --> Batch[batchCollector.add]
  Batch --> Emb[Embeddings]
  Emb --> Zvec[zvec UpsertChunks]
```

1. **`Coordinator.indexFile`** loads the active embedding profile (`max_input_tokens`, `embed_budget_ratio`), builds a `TokenCounter`, and passes `chunk.Options` into **`ProcessBatches`**.
2. **`ChunkRouter.ChunkFile`** chooses the strategy:
   - `strategy: line_window` → `line_window` for every file.
   - `hybrid` or `prose` + **prose extension** (`.md`, `.markdown`, `.mdc`, `.txt`) → **`prose.ChunkFile`** / **`prose.StreamBatched`** (no tree-sitter; works in shipped binary). Sets `chunk_strategy: prose` or `partial`, `chunk_type: markdown`, `symbol_kind` e.g. `section`, `paragraph`.
   - `hybrid` + AST extension + matching `languages.*.enabled` → **`ast.ChunkLanguage`** when the binary includes build tag **`treesitter`**; otherwise transparent fallback to `line_window` (`ErrNotImplemented`).
   - **1C BSL** (`.bsl`, `.os`) with `languages.bsl.enabled` → **`tree-sitter-bsl`** AST (procedures, functions, regions, module vars). With `languages.bsl.include_sdbl`, embedded query strings in BSL are stripped (`|` prefixes, quotes) and passed to the **heuristic SDBL chunker** (not tree-sitter).
   - **`.dcs`** (Data Composition Schema): when `include_sdbl: true`, the router regex-extracts `<query>...</query>` blocks → heuristic SDBL chunker (`chunk_type: query`); remaining XML → `line_window`. Without `include_sdbl`, `.dcs` is `line_window` only.
   - Extensions without a registered grammar (e.g. `.mdo`) → `line_window`.
3. **Streaming vs whole-file read:** files larger than `stream_chunk_threshold_bytes` normally use streaming `line_window`. **Exceptions:** (1) hybrid AST paths (`hybridASTPath`) — including `.bsl`, `.os`, and `.dcs` when `languages.bsl.include_sdbl` — read the file whole (up to `max_file_bytes`) for tree-sitter or DCS query extraction; (2) **prose paths** (`hybridProsePath`) — large `.md`/`.txt` files use `prose.StreamBatched`, not line streaming.

**ExtToLang / parser mapping** (`internal/indexer/chunk/router.go`):

| Extension | Language key | Parser |
|-----------|--------------|--------|
| `.go` | `go` | `tree-sitter-go` |
| `.py` | `python` | `tree-sitter-python` |
| `.js`, `.mjs`, `.cjs` | `javascript` | `tree-sitter-javascript` |
| `.jsx` | `tsx` | `tree-sitter-typescript` (`LanguageTSX`) |
| `.ts` | `typescript` | `tree-sitter-typescript` (`LanguageTypescript`) |
| `.tsx` | `tsx` | `tree-sitter-typescript` (`LanguageTSX`) |
| `.bsl`, `.os` | `bsl` | `tree-sitter-bsl` |
| `.dcs` | — | Not in `ExtToLang`. When `languages.bsl.enabled` and `include_sdbl: true`, `ChunkRouter.chunkDCS` regex-extracts `<query>…</query>` → **heuristic SDBL** (`ChunkSDBLText`); stripped XML → `line_window`. Requires `treesitter` build for query chunks; without it, whole file falls back to `line_window`. |

Config key `languages.typescript.enabled` also enables `.jsx` and `.tsx` (router maps `tsx` lang to the typescript toggle).

**SDBL note:** query text from `.dcs`, embedded BSL strings, or standalone SDBL is chunked by **heuristic** split on `;` and `ВЫБРАТЬ`/`SELECT` boundaries — not by tree-sitter (the BSL grammar bundle does not expose SDBL query nodes in v0.1.6).
4. **cAST emit:** chunks stream through `emit func(*zvec.Chunk) error` → `batchCollector.add` (no per-file `[]Chunk` slice in production).
5. **Context prefix:** optional `indexing.chunking.context_prefix` prepends `// file:` / `// scope:` to **embed input only**; stored `snippet` is raw source.
6. **Partial fallback:** oversized AST nodes split via `line_window` inside the parent scope; `chunk_strategy: partial`.
7. **Identity:** `index_meta.json` stores `chunking_version` / `chunking_strategy`; mismatch → `identity_mismatch` → `reindex` with `force: true`.

Shipped Release/install binary uses `-tags "zvec,onnx"` without `treesitter`: prose extensions still use the prose chunker; code extensions fall back to `line_window`. See [CONFIG.md](CONFIG.md#indexingchunking) and [DEVELOPMENT.md](DEVELOPMENT.md#tree-sitter-hybrid-ast-chunking).

## Resilience

| Mechanism | Purpose |
|-----------|---------|
| Idempotent zvec open | Avoid double-open LOCK errors |
| SIGTERM handler | Close collection, remove lock |
| stdio client disconnect | Exit process after MCP session ends (`awaitTransportResults`); cancels ctx and stops file watcher |
| stdio parent watch | Exit when a stdio launch-chain ancestor (`powershell`/`cmd` or Cursor) dies |
| Stale process cleanup | `internal/lifecycle` kills prior `--stdio` instance by exe + workspace path on startup |
| stdio.lock singleton | Block second `--stdio` process for same workspace |
| zvec lock recovery | On LOCK error: kill duplicate stdio, close handle, retry open (`SemanticSearch`, `index_status`) |
| Partial search during write | `indexing` metadata in search response; `/ready` stays not-ready until idle |
| `/health` vs `/ready` | Liveness vs embeddings+index loaded |
| Polling file watcher | Windows Docker bind-mount compatibility |
| Request context | MCP/HTTP pass `ctx` to sync service calls (search, status, ready); embeddings honor cancel; search uses **non-blocking context cancellation** — client returns on cancel/timeout while the zvec goroutine finishes in the background (`searchWG`) |
| Panic recovery | Search, background indexing, and workspace init catch panics to prevent process crash |
| Crash-safe deletion rows | Reindex/purge deletes zvec chunks before SQLite manifest rows so a crash cannot orphan vectors with no manifest trail |
| Indexing lifecycle | Background `Coordinator.run` uses process/workspace ctx; stops on shutdown/eviction, not on reindex client disconnect |

## Security

- Workspace mounted read-only in Docker.
- HTTP `API_TOKEN` Bearer auth (constant-time compare). When unset, the API is
  unauthenticated and a warning is logged on startup — loudly if the listen
  address is non-loopback (network-reachable). Per-project `--http` defaults to
  loopback (`127.0.0.1:8080`); daemon/Docker default `:8080`. Bind to
  `127.0.0.1` or set a token for network exposure.
- Shared daemon **open mode** (no `API_TOKEN`): `GET /v1/status`, `POST /v1/search`,
  and `POST /v1/reindex` redact workspace path fields and sanitize path-bearing
  messages (see [API.md — Open daemon redaction](API.md#open-daemon-redaction)).
- `GET /v1/workspaces` returns only workspace `id` and `open` by default; absolute
  paths from `daemon.yaml` require `?include_paths=1` **and** a valid Bearer token
  (`401` when token is unset or header missing/invalid).
- Secrets from `.env` (anything matching `*TOKEN*`, `*SECRET*`, `*PASSWORD*`,
  `*API_KEY*`, `*CREDENTIAL*`) are loaded into a private in-memory map and are
  **not** exported to the process environment (`os.Setenv`), so they don't leak
  via `/proc/<pid>/environ` or to child processes. Plaintext `api_key` in
  `config.yaml` is discouraged and logs a warning — prefer `api_key_env` + `.env`.
- Index contains source snippets — keep `data/` out of git; `data/` and `logs/`
  are created with `0700`/`0600` permissions.
- Default embeddings via local HTTP need no cloud API keys.

## Versioning

- Go module + `internal/version.Version`
- Git tags `v*`, GitHub Releases with cross-compiled binaries
- Docker: `ghcr.io/1CSerg/mcp-semantic-search-zvec-go:<semver>`
