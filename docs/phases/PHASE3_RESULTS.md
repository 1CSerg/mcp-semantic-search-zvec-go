# Phase 3 Results

## Scope delivered

- `internal/watcher` — in-process file watcher with `fsnotify` and `polling` backends, debounced incremental `reindex`
- `internal/service/searchstats.go` — rolling search latency metrics; `performance.degraded` / `performance.slow` in search JSON
- `internal/logging` — stderr + rotating `data/logs/server.log`
- `internal/crash` — `data/logs/last_crash.json` on panic
- `internal/indexer/recover.go` — stale `progress.json` recovery and stall detection
- `internal/lifecycle` — stale stdio cleanup + stale `index.lock` reclaim on startup
- `/ready` — index_meta, zvec collection, embeddings health probe (`GET /v1/models`)
- Phase 3 config env overrides wired in `internal/config`

## Automated gate

| Check | Command | Result |
|-------|---------|--------|
| Unit tests | `go test ./...` | Pass |
| Coverage ≥88% `./internal/...`, ≥50% на пакет | `make test-cover-check` | Pass |
| Phase 3 smoke (default 10 reconnect cycles) | `scripts/smoke/run-phase3.ps1` / `make smoke-phase3` | Pass (2026-06-08) |
| Phase 3 gate (50 reconnect cycles) | `.\scripts\smoke\run-phase3.ps1 -Reconnects 50` | Pass (2026-06-08) |

Smoke validates:

1. Repeated HTTP start/stop without stale `index.lock`
2. `/ready` after initial `reindex`
3. Polling watcher triggers incremental reindex on file change
4. Search response includes `performance` metrics

## 50× Reconnect Gate

Scripted equivalent passed on Windows with:

```powershell
.\scripts\smoke\run-phase3.ps1 -Reconnects 50
```

The script validates repeated process start/stop, `/ready`, absence of stale `index.lock`, watcher-triggered reindex, and search performance metrics.

For an IDE-level manual check in a target project with MCP wired via install:

1. Restart Cursor MCP server 50 times (toggle MCP off/on or reload window).
2. After each cycle: `index_status` → `indexing.running` is false when idle.
3. Verify no orphan `mcp-semantic-search-zvec-go` process for the workspace (Task Manager / `Get-Process`).
4. Verify `.mcp-semantic-search-zvec-go/data/index/index.lock` is absent when indexing is idle.

`internal/lifecycle.PrepareStdio` stops prior stdio instances and reclaims stale locks on each MCP spawn.

## Known limitations

- `file_watcher.run_as_daemon: true` is reported as unsupported until Phase 5 shared daemon.
- Embeddings health probe uses `GET /v1/models`; providers without that route may show `/ready` not ready even when `/v1/embeddings` works.
- The 50× gate was validated with HTTP process reconnects; a Cursor UI reconnect run can still be used as an additional release confidence check.
