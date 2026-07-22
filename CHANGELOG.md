# Changelog

## Unreleased

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
