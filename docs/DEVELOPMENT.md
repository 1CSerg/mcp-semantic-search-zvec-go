# Development

## Requirements

- Go 1.26+
- Git
- CGO toolchain (for zvec-go): GCC on Linux, MSVC or MinGW on Windows
- Optional: golangci-lint, Docker

## Clone and build

Первый `go build` без тегов — **stub** (unit tests, health/status; без zvec и ONNX). Для семантического поиска: `make fetch-zvec-libs && make build-zvec` (`-tags "zvec,onnx,treesitter"` — то же, что GitHub Release и install). При `strategy: hybrid` shipped-бинарник индексирует `.md`/`.markdown`/`.mdc`/`.txt` через **prose** chunker; enabled code langs (`.go`, `.py`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.ts`, `.tsx`, `.bsl`, `.os`) — через AST cAST; остальные расширения — `line_window`.

```bash
git clone https://github.com/1CSerg/mcp-semantic-search-zvec-go
cd mcp-semantic-search-zvec-go
make setup-hooks   # один раз: git add на Windows без fatal LF/CRLF
go build -o bin/mcp-semantic-search-zvec-go ./cmd/mcp-semantic-search-zvec-go   # stub
go test ./...
```

Windows (PowerShell): `.\scripts\setup-git-hooks.ps1` вместо `make setup-hooks`.

Окончания строк (LF):

- `.gitattributes` (`eol=lf`) — при `git add` в индекс попадает LF (clean-фильтр Git).
- `setup-hooks` выставляет `core.autocrlf=false` и `core.safecrlf=false` **для этого репо** — без этого на Windows `git add` падает с `LF/CRLF would be replaced`.
- `git addnorm` (= `scripts/dev/git-add.sh`) дополнительно приводит рабочую копию к LF перед индексацией. Обычный `git add` после `setup-hooks` тоже работает; встроенные `add`/`stage` через alias переопределить нельзя.

Dev mode uses repo `config.yaml` automatically when install tree is absent.

Path containment (`INDEX_DIR` / `CONFIG_PATH` / `daemon.yaml`): `internal/config/paths.go`, env `MCP_PATH_CONTAINMENT`, tests `go test ./internal/config/... -run Containment`.

Секреты в dev: скопируйте `templates/env.example` в `.mcp-semantic-search-zvec-go/.env` или задайте `ENV_PATH`. Бинарник загружает `.env` при старте до чтения профилей.

## Run modes

Windows: no CLI flags → help text, exit 0. Linux/macOS: no CLI flags → help text, exit 0. A run mode is always explicit; `--version` / `-version` prints version and exits.

```bash
# Help (same text as a bare invocation, no configuration is read)
./bin/mcp-semantic-search-zvec-go
./bin/mcp-semantic-search-zvec-go --help

# MCP stdio (Cursor spawns this explicitly)
./bin/mcp-semantic-search-zvec-go --stdio

# HTTP only (default bind 127.0.0.1:8080)
./bin/mcp-semantic-search-zvec-go --http

# Both (HTTP in background goroutine + MCP stdio)
./bin/mcp-semantic-search-zvec-go --stdio --http
```

Per-project `--http` and Cursor `--stdio` both acquire `stdio.lock` on the same `INDEX_DIR`. **Do not** run a separate `./bin/mcp-semantic-search-zvec-go --http` process while Cursor MCP is active for the same workspace — use `--stdio --http` in one process, or stop Cursor MCP first.

```bash
# Shared daemon (multi-workspace HTTP; default bind :8080)
./bin/mcp-semantic-search-zvec-go --daemon --daemon-config /path/to/daemon.yaml --http-addr :8080

# MCP stdio proxy to shared daemon (Cursor multi-repo)
./bin/mcp-semantic-search-zvec-go --stdio-proxy --workspace-id=my-app --daemon-url=http://127.0.0.1:8080

# Override config path
./bin/mcp-semantic-search-zvec-go --stdio --config /path/to/config.yaml
```

The per-project service layer (`--stdio`, `--http`) starts auto-indexing and the file watcher. When Cursor already runs `--stdio` for the same workspace, the second process reclaims orphan locks and surfaces the competing instance through `index_status.diagnostics` (`duplicate_stdio_suspected`, live `stdio.lock` holder from `FindStdioForWorkspace`) instead of competing for the index; `--stop-stdio-for-workspace <workspace>` terminates the stale instance.

If indexing was **interrupted** (process stopped mid-run), the next start reclaims orphan zvec `LOCK` files, migrates `progress.json` from `context canceled` to idle interrupted state, and automatically runs incremental `reindex` to continue (manifest skips already indexed files). Shutdown waits for the background indexer **and in-flight searches** before closing zvec to avoid orphan locks.

Full flag table: [API.md](API.md#cli-flags).

Logs go to stderr and `.mcp-semantic-search-zvec-go/logs/server.log` (rotation via `logging.max_bytes` / `logging.backup_count`). Fatal panics write `logs/last_crash.json` under the install dir.

## Project layout

```
cmd/mcp-semantic-search-zvec-go/   # main
internal/
  config/                           # YAML + env
  service/                          # Core API
  transport/mcp/                    # MCP stdio
  transport/http/                   # REST
  store/zvec/                       # zvec-go vector store
  embeddings/                       # openai_compatible, onnx
  indexer/                          # scan, chunk, coordinator
    chunk/                          # ChunkRouter, line_window, ProcessBatches
      prose/                        # Markdown/plain prose chunker (Phase 1e)
      ast/                          # cAST engine, tree-sitter (build tag treesitter)
  watcher/                          # fsnotify + polling
  logging/                          # file log rotation
  crash/                            # last_crash.json
  daemon/                           # multi-workspace registry (BorrowService, LRU, Close drain)
docs/
scripts/                            # install, fetch, dev, smoke, spike (see scripts/README.md)
templates/                          # MCP fragments
```

### Shared daemon (contributors)

`internal/daemon/registry.go` — lazy workspace open, LRU eviction, `Close()` drain. HTTP handlers in `internal/transport/http/server.go` call `redactIfOpenDaemon` when `daemon` mode runs without `API_TOKEN` (strips path fields, sanitizes path-bearing text in status/search/reindex JSON). Tests: `internal/transport/http/server_test.go` (`TestRedactDaemon*`, `TestDaemonMode*`).

## zvec-go

Vector store uses official [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) v0.7.0 (CGO, vendor pre-built libs; native core [alibaba/zvec](https://github.com/alibaba/zvec) bundled with that release). Где зафиксирована версия и как её менять — [Versions](#versions) (подраздел **zvec-go**).

### Build tags

| Command | Tags | Result |
|---------|------|--------|
| `go test ./...` | default (`!zvec`) | Stub store, no native deps |
| `make build-zvec` | `zvec,onnx,treesitter` | **Shipped** production binary (Release, install scripts): zvec + ONNX + tree-sitter; full hybrid (prose + AST for enabled langs) |
| `go build -tags "zvec,onnx"` or `zvec,!treesitter` | without `treesitter` | Legacy/fallback: prose + `line_window` for code when AST unavailable |
| `go test -tags "zvec,treesitter" ./internal/indexer/chunk/...` | `zvec,treesitter` | AST chunking tests (CGO + tree-sitter) |
| `make test-integration` | `integration,zvec` | Spike gate tests |

### Native deps (one-time per machine / after clean)

```bash
make fetch-zvec-libs
# writes .deps/zvec-lib.env with ZVEC_LIB_DIR
```

Clones [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) tag `v0.7.0` into `.deps/zvec-go` and downloads pre-built libs from GitHub Releases. `go.mod` uses `replace => ./.deps/zvec-go`.

### tree-sitter (hybrid AST chunking)

AST chunking uses [go-tree-sitter](https://github.com/tree-sitter/go-tree-sitter) **v0.25+** (ABI 15 for `tree-sitter-bsl`) with build tag **`treesitter`** (requires CGO, same toolchain as zvec-go). Registered grammars: **go**, **python**, **javascript**, **typescript**, **tsx** (`.jsx` → tsx parser), **bsl** (`.bsl`, `.os`; SDBL via heuristic chunker when `include_sdbl: true`). **Release/install binaries include `treesitter`** in production tags.

| Build | Tags | Behavior |
|-------|------|----------|
| Stub | (none) | No zvec, no AST; `ast.ChunkLanguage` returns `ErrNotImplemented` |
| Shipped / Release / install | `zvec,onnx,treesitter` | Prose extensions → prose; enabled code langs → AST cAST; `.dcs` query blocks when `bsl.include_sdbl`; others → `line_window` |
| Legacy fallback (CI) | `zvec,onnx` or `zvec,!treesitter` | Prose extensions → prose chunker; code extensions → `line_window` when AST unavailable |

Verify CGO + grammar linkage (spike gate):

```bash
bash scripts/fetch/fetch-tree-sitter-grammars.sh
# with zvec libs on PATH/LD_LIBRARY_PATH:
source .deps/zvec-lib.env
export CGO_ENABLED=1 LD_LIBRARY_PATH="$ZVEC_LIB_DIR:$LD_LIBRARY_PATH"
go test -tags "zvec,treesitter" ./internal/indexer/chunk/...
```

Windows (PowerShell):

```powershell
.\scripts\fetch\fetch-zvec-libs.ps1   # if not yet fetched
.\scripts\fetch\fetch-tree-sitter-grammars.ps1
# prepends ZVEC_LIB_DIR to PATH when .deps/zvec-lib.env exists
go test -tags "zvec,treesitter" ./internal/indexer/chunk/...
```

Grammars are vendored via Go modules (`tree-sitter-go`, `tree-sitter-python`, `tree-sitter-javascript`, `tree-sitter-typescript`, `tree-sitter-bsl` via `replace` → `github.com/alkoleft/tree-sitter-bsl`); fetch scripts only verify compile/link. CI runs `treesitter-chunk` on Linux and AST chunk tests on Windows (`test-windows` job).

**Hybrid chunk benchmark gate** (`internal/indexer/chunk/benchmark_test.go`, tags `zvec,onnx,treesitter`):

```bash
BENCH_CHECK=1 go test -tags "zvec,onnx,treesitter" -run TestBenchmarkHybridWithin2x ./internal/indexer/chunk/...
BENCH_CHECK=1 BENCH_FULL=1 go test -tags "zvec,onnx,treesitter" -run TestBenchmarkHybridWithin2x ./internal/indexer/chunk/...  # 1000/200/200 fixtures
python scripts/dev/generate-chunk-benchmark-fixtures.py /tmp/chunk-bench --go 1000 --tsx 200 --bsl 200
```

Gate compares hybrid vs `line_window` wall time and `HeapInuse` delta (limit 2×). CI `treesitter-chunk` runs the reduced set (50/10/10) in the main chunk test step, then a blocking `BENCH_FULL=1` gate on 1000/200/200 fixtures (hybrid must stay within 2× line_window).

### ONNX (local offline)

Local embeddings use [onnxruntime_go](https://github.com/yalue/onnxruntime_go) with build tag `onnx`.

```bash
make fetch-onnx-runtime   # .deps/onnxruntime.env
make fetch-onnx-model     # default bundle under .mcp-semantic-search-zvec-go/models/...
make build-zvec           # -tags "zvec,onnx,treesitter"
```

Env:

| Variable | Description |
|----------|-------------|
| `ONNXRUNTIME_SHARED_LIBRARY_PATH` | Path to `libonnxruntime.so` / `.dylib` / `.dll` |
| `ORT_LIB_DIR` | Directory containing ONNX Runtime library |
| `ONNXRUNTIME_VERSION` | Override runtime version in fetch scripts (default `1.26.0`) |
| `ONNX_MODEL_SHA256` | Optional checksum for `model_optimized.onnx` |
| `ONNX_TOKENIZER_SHA256` | Optional checksum for `tokenizer.json` |

Linux / macOS runtime:

```bash
source .deps/zvec-lib.env
export LD_LIBRARY_PATH="$ZVEC_LIB_DIR:$LD_LIBRARY_PATH"
```

Windows: CGO via **WinLibs MinGW gcc** (recommended) or VS **clang-cl**; plain `cl` rejects cgo `-Werror`. Scripts:

```powershell
.\scripts\dev\build-release.ps1      # production release (-ldflags -s -w)
.\scripts\dev\build-zvec-windows.ps1 # dev build (same tags, no strip)
```

Linux / macOS release: `make build-release` or `bash scripts/dev/build-release.sh`.

`zvec_c_api.dll` must be next to the executable or on `PATH` (script copies to `bin/`).

**Spike in Docker (Linux amd64):**

```powershell
docker run --rm -v "${PWD}:/src" -w /src golang:1.26.3-bookworm bash /src/scripts/spike/run-docker-inner.sh
```

First run ~2–3 min (clone + download libs). Covers integration checklist below.

**Zvec search smoke** (`make smoke-phase1` — seed-index → HTTP search, mock embeddings):

```powershell
.\scripts\smoke\run-phase1.ps1
```

Linux: `make smoke-phase1`

The smoke runs in an isolated workspace (`%TEMP%\mcp-zvec-smoke-phase1` on Windows,
`/tmp/mcp-zvec-smoke-phase1` on POSIX) with its own `config.yaml`, index and logs, so it
never touches your working index. It asserts `zvec_open_ok` / `zvec_doc_count` before
searching, covers `path_glob`, and prints the server log tail on failure.

**Windows: run the `.sh` scripts with Git Bash, not WSL.** If WSL is installed,
`bash.exe` on `PATH` is usually `C:\Windows\system32\bash.exe` (WSL), which has a Linux
`PATH` without Go or MinGW — the script then dies with `go: command not found` or
`exit status 127`. Use Git Bash explicitly:

```powershell
& "C:\Program Files\Git\bin\bash.exe" scripts/smoke/run-phase1.sh
```

Make targets that call `bash` have the same problem. Either put Git Bash first on `PATH`,
or override the `BASH` variable (quoted, because the path contains spaces):

```powershell
make smoke-phase1 BASH="C:/Program Files/Git/bin/bash.exe"
```

`fetch-zvec-libs`/`fetch-onnx-runtime` write their env file via a temp file + `mv`, so a
failed fetch no longer leaves a truncated `.deps/zvec-lib.env` behind.

**Indexer smoke** (`make smoke-phase2` — empty project → reindex → HTTP search):

```powershell
.\scripts\smoke\run-phase2.ps1
```

Linux: `make smoke-phase2`

**Resilience smoke** (`make smoke-phase3` — reconnect, watcher, `/ready`, search metrics):

```powershell
.\scripts\smoke\run-phase3.ps1
```

Linux: `make smoke-phase3`

**ONNX smoke** (`make smoke-phase4` — local `local_multilingual`, no external embedding API):

```powershell
.\scripts\smoke\run-phase4.ps1
```

Linux: `make smoke-phase4`

**Shared daemon smoke** (`make smoke-phase5` — 3 workspaces, MCP proxy unit check):

```powershell
.\scripts\smoke\run-phase5.ps1
```

Linux: `make smoke-phase5`

**Windows multi-project smoke** (project-local bin, Cyrillic path, `mcp.json` wiring):

```powershell
.\scripts\smoke\run-mcp-staging-multi-windows.ps1
```

Makefile: `make smoke-staging-multi-windows`

**Global install layout smoke** (no native tags required — builds a stub binary):
one application per user, N projects, per-project isolation, id/root conflicts,
idempotent re-install, unicode paths, daemon lifecycle, legacy-migration guard.
Runs entirely in a temporary `MCP_GLOBAL_DIR` on a free loopback port.

```powershell
.\scripts\smoke\run-global-install.ps1
```

```bash
bash scripts/smoke/run-global-install.sh     # Git Bash works on Windows too
```

Makefile: `make smoke-global-install`

**Global daemon native end-to-end** (production binary, `-tags zvec,onnx,treesitter`):
a mock embeddings server, one shared daemon, real MCP stdio proxies performing
`initialize`/`tools/list`/`tools/call`, three collections plus a Cyrillic path,
per-result source attribution (each hit must resolve to its own project and carry
that project's marker), adding a project while others run, proxy close/reopen,
id/root conflict refusal, a global profile change requiring a rebuild, and a
daemon restart.

```powershell
.\scripts\smoke\run-global-daemon.ps1
```

```bash
bash scripts/smoke/run-global-daemon.sh --binary ./bin/mcp-semantic-search-zvec-go
```

Makefile: `make smoke-global-daemon`

Driver: `scripts/smoke/global-daemon-check.go` (build-tag `ignore`, run with
`go run`). It stops the daemon it started through `--stop-daemon`; the wrappers
add a lock-file-based safety net.

Production build:

```bash
make build-zvec
make seed-index
./bin/mcp-semantic-search-zvec-go --http
```

### Integration checklist

Run on **Windows amd64** and **Linux amd64** after bump zvec-go or schema changes:

```bash
make fetch-zvec-libs
export CGO_ENABLED=1
# Linux/macOS: source .deps/zvec-lib.env && export LD_LIBRARY_PATH="$ZVEC_LIB_DIR:$LD_LIBRARY_PATH"
make test-integration
```

| # | Test | Pass criteria |
|---|------|---------------|
| 1 | Create collection with schema below | No CGO/link errors |
| 2 | Insert 100 docs with fp32 vectors | `doc_count` matches |
| 3 | Vector query top-k | Results ordered by score |
| 4 | Open existing collection read-only | Second open in same process idempotent |
| 5 | Delete by doc id | Count decreases |
| 6 | Graceful `Close()` then reopen | No LOCK file error |
| 7 | Kill -9 then stale lock handling | Next process reclaims (app-level) |
| 8 | Windows MSVC/MinGW build | Binary runs on target host |

Code: `internal/store/zvec/store.go`, tests in `store_integration_test.go` (`TestIntegrationSpikeChecklist`, tags `integration,zvec`). Windows Cyrillic path smoke: `store_cyrillic_integration_test.go` (`TestIntegrationCyrillicIndexDir`).

**Contributor note (Windows LOCK):** if zvec reports `Can't open lock file` or `Can't lock read-only collection`, investigate duplicate per-project MCP processes (`--stdio`, `--http`) and stale LOCK reclaim (`PrepareWorkspaceLocks`, `PrepareStdio`, `openZvecWithRecovery`) before assuming path encoding failure. **Do not** relocate zvec collections to `%LOCALAPPDATA%` or another ASCII-only root solely because `INDEX_DIR` contains Cyrillic characters.

| Outcome | Action |
|---------|--------|
| All pass | OK to ship zvec bump |
| Windows link fails | `zvec_c_api.dll` next to exe; see Windows CGO notes above |
| Schema incompatible | Indexes require `reindex` with `force: true` |
| Vendor libs GLIBC mismatch | Fallback: zvec-go source mode (`-tags source`) |

### Index compaction (zvec block growth)

zvec finalizes its memory component and allocates a **fresh on-disk block on every
`Flush()`**, and this store flushes after every upsert/delete batch. An unmaintained
index therefore grows one `.proxima` block per batch regardless of how few documents
that batch held.

Measured on Windows amd64 with the shipped `zvec_c_api.dll` (fixture: 256 batches x 4
docs, one flush per batch — `TestCompaction*` in `compaction_integration_test.go`):

| State | `.proxima` files | `.proxima` bytes | Whole collection |
|-------|------------------|------------------|------------------|
| Unmaintained (256 flushes) | 256 | 1 349 517 312 | 1 350 646 011 |
| After one native `Optimize()` | 1 | 4 182 016 | 5 274 658 |
| Store with the compaction policy | 1 | 5 226 496 | 5 952 783 |

Every flush allocated a block of **5 271 552 bytes** for 4 documents, so 1024 documents
cost 1.35 GB. `scalar.N.parquet` blocks (~2.7 KB each) also accumulate but `Optimize()`
does not reclaim them; that is ~1.4 MB in the same fixture.

**Policy** (`internal/store/zvec/collection.go`):

- `CollectionStore` keeps `dirty` and `writeOps`. `writeOps` counts **native
  write/delete operations**, not files, chunks or bytes.
- A periodic compaction runs after `compactionWriteThreshold = 128` durable operations.
  `128` is a starting compromise, not a guarantee of bounded size or time on an
  arbitrary index: smaller multiplies full rebuilds over a growing index, larger leaves
  the peak footprint high.
- Only a **durable** outcome (successful `Flush`) may trigger it, so the failed-flush
  branch never starts a rebuild. Partial and ambiguous outcomes still set `dirty`.
- `Compactor` (`Compact() error`) is an **optional** interface, deliberately outside
  `Store`: existing stores, mocks and the `!zvec` stub keep compiling. `StubStore`
  returns `ErrNotLinked`.
- `Compact()` on a clean or missing collection is a no-op and never creates a
  collection, so an **unchanged run performs zero native optimize calls**.
- A failed pass logs `slog.Warn`, keeps `dirty` and restarts the 128-operation interval,
  so it is not retried on every following batch.
- `WipeCollection` resets both counters (the collection they described is gone);
  `Close` keeps them (they describe the on-disk collection, not the handle).

**Coordinator** (`internal/indexer/coordinator.go`): `compactStoreBestEffort` runs the
optional pass through a type assertion.

- A normal run finalizes the active collection on every exit path, including a fatal
  error after partial changes; the maintenance error never replaces the run's own
  error, and `Running=false` is not published until it finishes.
- A force reindex maintains the **staging** build inside `promoteStaging`, i.e. before
  `Close` and before the rename, so the published index is never the unmaintained one.
  The active collection it replaces is not optimized. A failed staging build is not
  compacted at all.
- A cancelled or shutting-down run does not start a new full rebuild.

**Cost of a pass:** it holds the store mutex for the whole native call, so search and
writes on that collection wait for it. The wait is proportional to the index size, not a
fixed short pause. In the fixture above (1024 docs) one `Optimize()` took ~80 ms and the
longest search during it ~97 ms; on a large index both grow. A pass runs every 128
durable write/delete operations, so the worst case is one rebuild of a growing index per
128 batches — the periodic schedule is what bounds the footprint at the price of that
pause. Moving the pass off the write path (mark "needs compaction" and let the indexer
schedule it) is a separate design decision, not part of this policy.

`Optimize()` also needs temporary free disk space: it rewrites the index while the old
blocks still exist. Size the index directory with that headroom.

**An error from compaction does not mean the index is wrong.** It means the footprint
was not reduced; documents, manifest and search are unaffected.

**Recovering an already bloated index:** the first-version answer is the existing
`reindex` with `force: true` — the new staging collection gets both the periodic and the
final compaction. It recomputes embeddings and needs disk space for staging, so it is
never started automatically. Do not delete `.proxima` files by hand and do not open a
working collection from a second process. Note that an **unchanged** run does not shrink
an old bloated index: the `dirty` flag only knows about writes made through this
process, so historical bloat needs the explicit force reindex.

Tests: `compaction_test.go` (policy unit tests, tag `zvec`),
`compaction_integration_test.go` and `compaction_acceptance_integration_test.go`
(native, tags `integration,zvec`), `coordinator_compaction_test.go` (coordinator
ordering and error semantics), `coordinator_delete_test.go` (delete retry semantics).

**Which build proves what:** the unit policy tests call `writeOutcomeLocked` /
`compactLocked` directly, so they stay green even if a write path stopped calling them.
The wiring from `UpsertChunks`/`DeleteByIDs` into the policy is proven only by the
native `integration,zvec` build (`TestCompactionOptimizeFailureDoesNotFailWrite`,
`TestCompactionAutomaticMaintenance`). Run that build, not just `-tags zvec`, before
trusting a change to the write path.

### See also

- [zvec-go examples](https://github.com/zvec-ai/zvec-go/tree/main/examples) — Go API reference
- [zvec-agent-skills](https://github.com/zvec-ai/zvec-agent-skills) — product concepts (Python/Node); hybrid search ideas for future work

Collection name: `ws_<sha256(workspace:profile:dims)[:16]>`. Fields:

| Field | Type |
|-------|------|
| `path` | string |
| `start_line` | int64 |
| `end_line` | int64 |
| `chunk_type` | string | `code` (default AST/line_window), **`markdown`** for prose (`.md`, `.markdown`, `.mdc`, `.txt`), or **`query`** for SDBL query text (heuristic chunker — not tree-sitter) |
| `name` | string |
| `snippet` | string |
| `symbol_name` | string |
| `symbol_kind` | string |
| `parent_scope` | string |
| `chunk_strategy` | string |
| `embedding` | vector fp32, N = profile dimensions |

Hybrid AST chunks populate `symbol_*`, `parent_scope`, and `chunk_strategy` (`ast`, `partial`, or `line_window`). **Prose** chunks (`.md`, `.markdown`, `.mdc`, `.txt`) set `chunk_type: markdown`, `chunk_strategy: prose` or `partial`, `symbol_kind` e.g. `section`, `paragraph` — works without `treesitter`. SDBL query chunks (`.dcs` `<query>` blocks, embedded BSL strings) set `chunk_type: query` and `symbol_kind: query` via the **heuristic** SDBL chunker — BSL itself uses **tree-sitter-bsl**. Legacy indexes may leave symbol fields empty until `reindex` with `force: true`.

## Cross-compile

Default `go build` (no tags): stub semantic backend without zvec/ONNX, pure Go on every OS — no desktop/CGO toolchain is needed for the stub.

With zvec (`-tags zvec`): use native runners per OS; avoid naive cross-compile with CGO.

## CI

- `.github/workflows/ci.yml` — `go test -race`, покрытие (≥80% `./internal/...`, ≥50% на пакет), `go vet`, job `zvec-integration` (`-tags integration,zvec`), `test-windows`, golangci-lint (blocking) on push/PR
- **Matrix (OS × build tags):** stub (no tags), `zvec,onnx` / `zvec,!treesitter`, `zvec,onnx,treesitter` on Ubuntu/Windows; optional `macos-latest` treesitter job (`continue-on-error`)
- **`treesitter-chunk` job:** AST chunk tests, blocking `BENCH_FULL=1` hybrid benchmark gate (1000/200/200 fixtures, ≤2× `line_window`), **≥80% coverage** on `internal/indexer/chunk` and `internal/indexer/chunk/ast` with `-tags "zvec,onnx,treesitter"`
- **`merge-config` job:** `python -m unittest scripts/install/merge-config_test.py`
- `.github/workflows/release.yml` — tag `v*` → binaries + Docker; platform archives upload directly to draft GitHub Release assets (not Actions artifacts); macOS release is `darwin/arm64` only (zvec-go prebuilts)

## Testing MCP locally

Use [MCP Inspector](https://github.com/modelcontextprotocol/inspector) or Cursor after wiring `.cursor/mcp.json`.

Quick HTTP smoke:

```bash
go run ./cmd/mcp-semantic-search-zvec-go --http &
curl -s http://127.0.0.1:8080/health | jq .
curl -s http://127.0.0.1:8080/v1/status | jq .
```

## `go test ./...` on Windows: PIE test binaries and security software

On a Windows host with an on-access antivirus/EDR agent, `go test ./...` can fail on
`internal/embeddings/openai` with either of these errors, while every other package passes:

```
github.com/1CSerg/mcp-semantic-search-zvec-go/internal/embeddings/openai.test:
open ...\go-build...\b001\openai.test.exe: Access is denied.
FAIL ... [build failed]

fork/exec ...\go-build...\b001\openai.test.exe: Access is denied.
FAIL github.com/1CSerg/mcp-semantic-search-zvec-go/internal/embeddings/openai
```

**Diagnosis (confirmed on Windows 11 + Go 1.26.3, Kaspersky running):**

- `go test` links the test binary with `-buildmode=pie -s -w` and then immediately runs
  `go tool buildid -w` on it; the run step follows right after. A scanner that opens the
  freshly linked executable first makes Go fail with `Access is denied` — sometimes at the
  `buildid` step (build failure), sometimes at `fork/exec` (run failure), depending on timing.
- The file's ACL is not the problem: it carries only inherited `Allow` entries for
  `SYSTEM` / `Administrators` / the user, and there is no deny ACE. `Copy-Item` of the same
  file is also denied, i.e. an open handle, not a permission rule.
- The artifact later disappears from the build directory (quarantine/removal by the agent).
- Content alone is not the trigger: `go test -c -o <path>` links the identical bytes
  (`-ldflags "-s -w"`, same 7 684 096-byte binary) and stays openable and executable.
- `internal/embeddings/openai` is the only affected package: its test binary is the largest
  in the tree (≈7.7 MB stripped) and starts an `httptest` listener, which is what the agent
  reacts to. Nothing in the package or the repo causes it.

**Supported local workaround — link a non-PIE test binary:**

```powershell
# one package
go test -ldflags=-buildmode=exe ./internal/embeddings/openai/

# whole tree
$env:GOFLAGS = "-ldflags=-buildmode=exe"; go test ./...
```

```bash
# Makefile targets run this for you (see below)
make test-local          # go test ./... with -ldflags=-buildmode=exe
make test-local-race     # same, with -race
```

`-buildmode=exe` only changes how the test binary is linked; tests, build tags and
behaviour are identical. **CI is not affected:** the Linux runners do not use this path
(`go test -v -race -count=1 ./...` stays as-is in `.github/workflows/ci.yml`), so do not
add `GOFLAGS` to the workflow.

`CGO_ENABLED=0`, `go clean -cache`, a different `GOTMPDIR` (including another disk) and
short paths do **not** help — the denial comes from outside the Go toolchain.

## Покрытие тестами

Пороги для `./internal/...` (встроенный `go tool cover`):

- **80%** — суммарно по проекту (`COVERAGE_MIN`)
- **50%** — минимум на каждый Go-пакет (`COVERAGE_PKG_MIN`)

```bash
make test-cover          # отчёт по функциям
make test-cover-check    # fail если ниже порогов
```

Windows: `.\scripts\dev\check-coverage.ps1`

Переопределение: `COVERAGE_MIN=90 COVERAGE_PKG_MIN=60 make test-cover-check`

HTML-отчёт: `go tool cover -html=coverage.out -o coverage.html`

## Contributing workflow

1. Branch from `main`.
2. `go test ./...` && `make test-cover-check` && `go vet ./...` && `gofmt -w .` (CI также гоняет `go test` на `windows-latest` без `-race`).
3. При изменении install/config merge: `python -m unittest scripts/install/merge-config_test.py` (нужен `pip install -r scripts/install/requirements.txt`).
4. Update docs if API/config changes.
5. PR with Russian commit messages (project convention) — см. `.cursor/rules/git-commits-ru.mdc`.

Конвенции для агентов в **этом репозитории**: `.cursor/rules/development.mdc` (gate-правила и MCP dogfooding при планировании/ревью).

Использование MCP в **целевом проекте пользователя** (install, tools, troubleshooting): [AGENTS.md](../AGENTS.md).

## Realworld harness (manual E2E)

Расширенные E2E-сценарии «как в проде» — **не** входят в `go test ./...`, CI и pre-commit.

```bash
make test-realworld              # ONNX offline (local_multilingual)
make test-realworld-lmstudio     # lmstudio_qwen; skip если LM Studio недоступен
bash scripts/realworld/run-all.sh --profile onnx --run TestChunking
bash scripts/realworld/run-all.sh --profile onnx --docker   # + Docker D1/D2 smoke
```

Windows: `.\scripts\realworld\run-all.ps1 -Profile onnx` (флаг `-Docker` для контейнерного smoke).

Prerequisites: `make build-zvec` (CGO, zvec libs, ONNX runtime); ONNX model подтягивается `setup-harness`. Ephemeral install tree: `.realworld/` (gitignored). Корпус: `tests/realworld/corpus/`. Wave 2: daemon/proxy, auth, concurrency, incremental lifecycle, search edge cases, CLI flags, Cyrillic paths (Windows), optional Docker. Manual-only: duplicate stdio diagnostics, headless no-args contract (CLI5 — also covered automatically by `TestCLIHeadlessContract`), Docker→LM Studio (D3). Подробнее: [tests/realworld/README.md](../tests/realworld/README.md).

## Versions

### MCP server

- Единственный источник версии: `internal/version/version.go`.
- `scripts/install/install.sh` и `scripts/install/install.ps1` читают версию из клона; install собирает бинарник (`go build`) или копирует готовый из `bin/`. Не добавляйте захардкоженный fallback в скрипты.
- Перед тегом `v*`: отредактируйте `version.go`; release workflow проверяет совпадение с git-тегом.
- Примеры установки в `AGENTS.md`, `README.md`, `INSTALL.md` — из актуального клона или release-тега.

### zvec-go

Версия [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) задаётся в нескольких местах — при bump обновляйте **все** перечисленные:

| Место | Что менять |
|-------|------------|
| `go.mod` | `require github.com/zvec-ai/zvec-go vX.Y.Z` и `replace => ./.deps/zvec-go` |
| `internal/version/version.go` | `ZvecGoVersion` (тег `vX.Y.Z`, вшит в бинарник) |
| `scripts/fetch/fetch-zvec-libs.sh` | default `ZVEC_GO_TAG` (сейчас `v0.7.0`) |
| `scripts/fetch/fetch-zvec-libs.ps1` | то же |
| `docker/Dockerfile` | `ARG ZVEC_GO_TAG=vX.Y.Z` |

Локальная копия модуля и pre-built native libs (`zvec_c_api.dll` / `libzvec_c_api.so` и т.д.) подтягиваются в `.deps/zvec-go` через `make fetch-zvec-libs` по тегу из fetch-скриптов. Переопределение без правки файлов: env `ZVEC_GO_TAG=vX.Y.Z`. Fetch-скрипты при несовпадении тега делают `git fetch` + `checkout -f` (re-clone вручную не обязателен) и накатывают ACP-патч Unicode-путей из [`scripts/fetch/patches/zvec-go-acp/`](../scripts/fetch/patches/zvec-go-acp/) (Windows Cyrillic `INDEX_DIR`).

После смены версии: обновите таблицу выше, при необходимости перегенерируйте `collection.go.patch`, и снова `make fetch-zvec-libs`.

**Автомиграция в целевом проекте:** при старте бинарник сравнивает `version.ZvecGoVersion` с `zvec_go_version` в `index_meta.json`. При расхождении сбрасывает zvec-коллекцию и `manifest.db`; если `AUTO_INDEX_ON_START=true` (Native install, per-project `--stdio`) — запускает force `reindex`, иначе нужен ручной MCP `reindex`. В shared daemon режиме auto-index при старте не действует.
