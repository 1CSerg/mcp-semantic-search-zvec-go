# Roadmap

## Phase 0 — Bootstrap ✅ (current)

**Goal:** Compilable repo, docs, CI skeleton, MCP + HTTP stubs.

| Deliverable | Status |
|-------------|--------|
| `go build ./...`, `go test ./...` | Done |
| MCP tools registered (stub responses) | Done |
| HTTP `/health`, `/v1/*` (stub) | Done |
| docs/, config.yaml, install templates | Done |

**Gate:** CI green on push.

---

## Phase 1 — zvec-go spike + read path ✅

**Goal:** Real `semantic_search` and `index_status` for existing indexes.

| Task | Criteria | Status |
|------|----------|--------|
| zvec-go spike | Open collection; query returns chunks — [ZVEC_SPIKE.md](ZVEC_SPIKE.md) | Done |
| `internal/store/zvec` | insert/query/delete, idempotent open (`-tags zvec`) | Done |
| `internal/embeddings/openai` | OpenAI-compatible HTTP batch embed | Done |
| `internal/store/manifest` | Read SQLite manifest | Done |
| Wire `service` | Phase1 search + status | Done |

**Gate:** `make seed-index` + `make build-zvec` → HTTP `/v1/search` returns ranked results.

---

## Phase 2 — Indexer write path

**Goal:** Full incremental indexing in Go.

| Task | Criteria |
|------|----------|
| `internal/indexer/scan` | git ls-files + walk; gitignore |
| `internal/indexer/chunk` | tree-sitter + markdown + window fallback |
| `internal/lock` | index.lock + progress.json |
| `reindex` MCP + HTTP | Background job, status polling |
| Install scripts | Production binary download, Cursor wiring |
| `check_update` | GitHub releases API |

**Gate:** Empty project → `reindex` → searchable in IDE.

---

## Phase 3 — Resilience + watcher

**Goal:** Production stability per-project mode.

| Task | Criteria |
|------|----------|
| File watcher | fsnotify + polling backend |
| Search metrics | slow/degraded hints in responses |
| Crash logging | `last_crash.json`, log rotation |
| In-binary stale stdio cleanup | `internal/lifecycle` on `--stdio` startup (done) |
| `/ready` | Fails until index + embeddings OK |

**Gate:** 50× Cursor reconnect without orphan processes or stale locks.

---

## Phase 4 — Local ONNX + Docker

**Goal:** Offline default profile; container release.

| Task | Criteria |
|------|----------|
| `internal/embeddings/onnx` | onnxruntime_go + bundled model |
| Docker multi-stage | GHCR image |
| Release workflow | Windows/Linux/macOS binaries attached |

**Gate:** `local_multilingual` profile works without external API.

---

## Phase 5 — Shared daemon + v1.0

**Goal:** One HTTP service, multiple projects; hardening.

| Task | Criteria |
|------|----------|
| `internal/daemon` | WorkspaceRegistry, `daemon.yaml` |
| HTTP `workspace_id` | All v1 routes multi-tenant |
| MCP `--stdio-proxy` | Cursor → shared daemon |
| `GET /v1/workspaces` | List registered workspaces |
| Load tests, API auth | v1.0.0 tag |

**Gate:** 3 workspaces on one daemon; MCP proxy from 2 Cursor projects.

---

## Timeline estimate

| Phase | Duration |
|-------|----------|
| 0 Bootstrap | 1 day |
| 1 Read path | 2–3 weeks |
| 2 Indexer | 3–4 weeks |
| 3 Resilience | 2 weeks |
| 4 ONNX/Docker | 2–3 weeks |
| 5 Daemon | 2–3 weeks |

**Total:** ~3–4 months (single maintainer).
