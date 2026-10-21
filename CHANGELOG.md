# Changelog

## Unreleased

- **Feature (default deployment change):** one application per user, one shared daemon, N project collections. The default install now places the binary, native libraries, models, config and secrets in a user-level directory (`%LOCALAPPDATA%\mcp-semantic-search-zvec-go`, `$XDG_DATA_HOME/...`, or `MCP_GLOBAL_DIR`) and stores only `project.yaml` (`schema_version`, `collection_id`) plus managed IDE entries inside each project. Legacy per-project installs remain available with `-McpMode Native` / `--mcp-mode native` and are unchanged.
- Add: project commands on the shared binary — `--project-add`, `--project-remove`, `--project-new-collection`, `--project-relocate` (needs `--yes`), `--projects-reload`, `--ensure-daemon`, `--stop-daemon`; and `--stdio-proxy --project-config <absolute project.yaml>` for project-aware MCP proxies that bind the collection id to every request.
- Add: `internal/daemon.ProjectManager` — UUID v4 collection ids, strict `projects.d` format (`id`/`root`/`description`), canonical root matching (spaces, Cyrillic, case, symlinks), atomic writes under a management lock, collision refusal (same id/different root, case-only ids, same root/different id), and data-preserving removal.
- Add: `internal/daemonclient` — one `auth.token` per user (32 random bytes, exclusive creation, never regenerated on corruption), `runtime/daemon.lock` singleton, detached daemon launch (Unix `Setsid`, Windows detached process group with file log handles — never bound to the proxy's context), authenticated `/health` liveness with a bounded wait, loopback-only validation, no redirect following, and `ErrPortInUse` when the port belongs to something else.
- Add: `POST /v1/admin/reload-projects` (atomic snapshot reload: full validation before apply, unchanged collections keep handles/jobs, 400 on an invalid snapshot, 409 when a changed collection is in use) and `POST /v1/admin/stop` (202 before graceful shutdown, never `os.Exit`). Admin routes are disabled when no `API_TOKEN` is configured.
- Add: strict `project.yaml` schema — unknown keys rejected at every level, `schema_version` (absent = 1, other values rejected), no path/endpoint/secret keys, pointer-based partial language overrides, and a final validation pass after the merge. Merged collection settings are independent objects: one project's overrides cannot leak into another's.
- Add: `daemon.yaml` `auto_index_on_open` (default true) wired to `PrepareStartup` for projects.d collections; an empty registry is now valid so the daemon can start before the first project.
- Add: GUI collection selector — lists the registered collections, switches the daemon target, cancels the previous collection's in-flight requests on switch, and never builds a `Phase1` or suggests stopping a concurrent proxy in the global layout.
- Add: install/uninstall split — `-InstallAppOnly`/`--install-app-only`, `-MigrateLegacy`/`--migrate-legacy` (opt-in; profile identity conflicts refuse instead of "last one wins"), `-UninstallApp`/`--uninstall-app` (refused while projects are registered unless forced). Binary replacement uses staging, `--version` verification, graceful daemon stop, and backup/rollback; it refuses (without force-killing) while a proxy still holds the binary.
- Add: `scripts/smoke/run-global-install.{ps1,sh}` (layout, isolation, conflicts, idempotency, unicode paths, lifecycle — 56 and 49 checks) and `scripts/smoke/run-global-daemon.{ps1,sh}` + `scripts/smoke/global-daemon-check.go` (native end-to-end: mock embeddings, real MCP `initialize`/`tools/call` over stdio, three collections, per-result source attribution, daemon restart, profile change).
- Fix: **the shared daemon killed its own MCP proxies.** `lifecycle.PrepareWorkspaceLocks` scanned for and stopped every process whose command line named the workspace root; for a projects.d collection the proxies name it via `--project-config`, so the first cold open terminated the sessions being served (`server is closing: EOF` on every tool call). The process-stop part now runs only for the legacy per-project layout.
- Fix: force reindex could fail on Windows with `rename active manifest: … being used by another process` when a concurrent `/v1/status` poll held `manifest.db` open; promotion renames now retry briefly.
- **Breaking:** `indexing.chunking.version` default is **4** — tighter `min_chunk_tokens` (24), `context_prefix` default **true**, AST boundaries honor min-token coalesce. Run `reindex` with `force: true` after upgrade.
- Search quality: over-fetch ANN results before rerank; stronger path/symbol boosts and demote of docs/testdata/micro-chunks; drop standalone tiny AST `const`/`var` boundaries below `min_chunk_tokens`.
- **Breaking (prior):** `indexing.chunking.version` **3** — DocIDs include byte offsets (AST/line_window/prose), chunk index, strategy/type, and content fingerprint.
- Fix: eliminate DocID collisions (manifest/zvec count desync); copy-on-write file updates with cleanup journal; partial native upsert/delete handling; staged force reindex (active index kept until staging succeeds).
- Fix: manifest/zvec desync detection via unique DocID counts and per-ID presence checks in zvec.
- Fix: `Phase1.Shutdown` retryable after timeout; daemon late `release` closes workspace; search panic → HTTP 500 / MCP tool error (no panic text).
- Fix: strict JSON body parsing (`MaxBytesReader`, trailing JSON rejected); `.env` path priority; `path_allowlist` on `CONFIG_PATH`; content hash in manifest; path redaction in `file_watcher.last_error`.
- Fix: Windows ACP path conversion fails on lossy character substitution; fetch scripts version-marker + SHA256 for native libs; install scripts refresh runtime via `.install-runtime-version`.
- Change: bump zvec-go pin to published tag `v0.7.0` (adds IVF RaBitQ / Vamana index types, RABITQ quantization, iterators); installed indexes with older `zvec_go_version` will reset and require force reindex.
- Fix: staged force reindex could not build at all — the staging collection was opened read-only (`ErrCollectionMissing` on a clean index) and promote renamed directories while the active collection still held open handles (Windows `Access is denied`). Staging now opens writable and promote releases both stores first, so the zvec-go migration reset/rebuild path works.
- Fix: `path_glob` lost recall and could return an empty result — the topK expansion doubled past the collection size (e.g. 64 → 128 with 121 docs) so no full pass was ever made, and with `limit` greater than the collection size the loop never ran at all. The schedule is now `min(topK, docCount)` capped at `docCount` (unit-tested via `globTopKStart`/`nextGlobTopK`, plus integration cases for `topK > docCount` and worst-rank matches).
- Fix: `cmd/seed-index` wrote a collection without `index_meta.json`, so the server classified it as a legacy zvec-go index and wiped it on startup — HTTP search then answered "index not found". The seed now writes a complete identity meta (`zvec.WriteIdentityMeta`).
- Fix: `scripts/smoke/run-phase1.{ps1,sh}` now run in an isolated `%TEMP%` workspace (own config/index/logs, no containment warnings), allocate free ports, use built helper binaries instead of `go run` children, assert `zvec_open_ok`/`zvec_doc_count` before searching, and cover `path_glob`; the `smoke-check` Go helper replaces ad-hoc JSON probing.
- Fix: `.deps/zvec-lib.env` is written with a quoted value when the path contains backslashes, so `source` no longer mangles a Windows `ZVEC_LIB_DIR`; the PowerShell readers strip the quotes.
- Fix: a stale `zvec_c_api.dll` in the repository root silently won the DLL search order and broke `go run -tags zvec ./cmd/seed-index` (`0xc0000139`). The fetch/build scripts now warn about root-level native libs whose hash differs from the pin.
- Fix: flaky `t.TempDir()` cleanup failures in `internal/service`, `internal/indexer` and `internal/store/zvec` tests — the coordinator now signals completion (`WaitForIdle`) only after its final `progress.json` write, and the tests delete their index dir with a Windows-aware retry that reports real leaks instead of an opaque "directory is not empty".
- Docs: document the Windows `go test ./...` PIE failure on `internal/embeddings/openai` (`Access is denied`, caused by an on-access scanner holding the freshly linked test binary) and the supported `-ldflags=-buildmode=exe` workaround (`make test-local`, `make test-local-race`); CI is unaffected.
- Fix: `make fetch-zvec-libs` / `make fetch-onnx-runtime` write their env file via a temp file + `mv`, so a failed fetch no longer leaves an empty `.deps/*.env` behind (the shell created the redirect target before the script ran).
- Fix: Makefile `BASH ?= bash` variable — on Windows with WSL installed, `bash` on `PATH` is WSL's bash without Go/MinGW; `make smoke-phase1 BASH="C:/Program Files/Git/bin/bash.exe"` now works.

## v0.3.0

- Fix: align `internal/version/version.go` `Version` with git tag `v0.3.0` (release workflow gate).
- Fix: revert zvec-go pin to published tag `v0.5.1` (premature `v0.6.0` was not released upstream).
- Change: drop GitHub Release `darwin/amd64` (Intel Mac); zvec-go v0.5.1 ships prebuilts for Apple Silicon only; `macos-13` runner retired Dec 2025.

## v0.2.0

- Fix: shared daemon `WorkspaceRegistry` — two-phase cold-open, `Close()` drain, `ErrRegistryClosing` → HTTP 503 during shutdown.
- Fix: open daemon (no `API_TOKEN`) redacts path fields in status/search/reindex; sanitizes path-bearing messages; `include_paths` requires Bearer.
- Docs: align log path to `.mcp-semantic-search-zvec-go/logs/server.log`.

## v0.1.8

- Fix: send `dimensions` in OpenAI-compatible embed API requests (MRL models such as Perplexity `pplx-embed-v1-4B`).
- Fix: `index_status.message` now reflects actual identity mismatch reason (profile/dimensions), not generic "workspace path changed".
- Fix: Windows GUI graceful shutdown — wait for background indexing, close zvec, reclaim orphan LOCK files.
- Fix: interrupted indexing (GUI close mid-run) resumes automatically on next GUI start (incremental reindex).
- Fix: HTTP embed timeouts no longer treated as lifecycle interrupt (avoid masking provider errors).
- Fix: agent docs log path — tail `.mcp-semantic-search-zvec-go/logs/server.log` (not `data/logs/`).
- Add: `index_status` identity fields (`active_profile`, `index_embedding_profile`, `identity_mismatch_reason`, …).
- Add: YAML parse hint when scalar values contain unquoted `:`.
- Add: `routerai_perplexity_4b` profile in template `config.yaml`.
- Add: Roo/Zoo Code install (`.roo/mcp.json`, `.roo/rules/semantic-search-zvec-go.md`).
- Add: merge-config warning for unquoted `description:` values with colons.
