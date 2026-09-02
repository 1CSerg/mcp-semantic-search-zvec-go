# Changelog

## Unreleased

- **Breaking:** `indexing.chunking.version` default is **4** — tighter `min_chunk_tokens` (24), `context_prefix` default **true**, AST boundaries honor min-token coalesce. Run `reindex` with `force: true` after upgrade.
- Search quality: over-fetch ANN results before rerank; stronger path/symbol boosts and demote of docs/testdata/micro-chunks; drop standalone tiny AST `const`/`var` boundaries below `min_chunk_tokens`.
- **Breaking (prior):** `indexing.chunking.version` **3** — DocIDs include byte offsets (AST/line_window/prose), chunk index, strategy/type, and content fingerprint.
- Fix: eliminate DocID collisions (manifest/zvec count desync); copy-on-write file updates with cleanup journal; partial native upsert/delete handling; staged force reindex (active index kept until staging succeeds).
- Fix: manifest/zvec desync detection via unique DocID counts and per-ID presence checks in zvec.
- Fix: `Phase1.Shutdown` retryable after timeout; daemon late `release` closes workspace; search panic → HTTP 500 / MCP tool error (no panic text).
- Fix: strict JSON body parsing (`MaxBytesReader`, trailing JSON rejected); `.env` path priority; `path_allowlist` on `CONFIG_PATH`; content hash in manifest; path redaction in `file_watcher.last_error`.
- Fix: Windows ACP path conversion fails on lossy character substitution; fetch scripts version-marker + SHA256 for native libs; install scripts refresh runtime via `.install-runtime-version`.
- Change: bump zvec-go pin to published tag `v0.6.0`; installed indexes with older `zvec_go_version` will reset and require force reindex.

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
