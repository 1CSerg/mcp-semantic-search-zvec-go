# Roadmap

## Phase 0 — Bootstrap ✅

**Goal:** Compilable repo, docs, CI skeleton, MCP + HTTP stubs.

| Deliverable | Status |
|-------------|--------|
| `go build ./...`, `go test ./...` | Done |
| MCP tools registered (stub responses) | Done |
| HTTP `/health`, `/v1/*` (stub) | Done |
| docs/, config.yaml, install templates | Done |

**Gate:** CI green on push — ✅ (2026-06-08).

---

## Phase 1 — zvec-go spike + read path ✅

**Goal:** Real `semantic_search` and `index_status` for existing indexes.

| Task | Criteria | Status |
|------|----------|--------|
| zvec-go spike | Open collection; query returns chunks — [ZVEC_SPIKE.md](spike/ZVEC_SPIKE.md) | Done |
| `internal/store/zvec` | insert/query/delete, idempotent open (`-tags zvec`) | Done |
| `internal/embeddings/openai` | OpenAI-compatible HTTP batch embed | Done |
| `internal/store/manifest` | Read SQLite manifest | Done |
| Wire `service` | Phase1 search + status | Done |

**Gate:** `make seed-index` + `make build-zvec` → HTTP `/v1/search` returns ranked results — ✅ (2026-06-08, `scripts/smoke/run-phase1.*`).

---

## Phase 2 — Indexer write path ✅

**Goal:** Full incremental indexing in Go.

| Task | Criteria | Status |
|------|----------|--------|
| `internal/indexer/scan` | git ls-files + walk; extensions/skip_dirs | Done |
| `internal/indexer/chunk` | window fallback (tree-sitter later) | Done |
| `internal/lock` | index.lock + progress.json | Done |
| `reindex` MCP + HTTP | Background job, status polling | Done |
| Install scripts | Production binary download, Cursor wiring | Done |
| `check_update` | GitHub releases API | Stub |

**Gate:** Empty project → `reindex` → searchable in IDE — ✅ (`scripts/smoke/run-phase2.*`).

---

## Phase 3 — Resilience + watcher ✅

**Goal:** Production stability per-project mode. Deliverables and gate evidence: [PHASE3_RESULTS.md](phases/PHASE3_RESULTS.md).

| Task | Criteria | Status |
|------|----------|--------|
| File watcher | fsnotify + polling backend | Done |
| Search metrics | slow/degraded hints in responses | Done |
| Crash logging | `last_crash.json`, log rotation | Done |
| In-binary stale stdio cleanup | `internal/lifecycle` on `--stdio` startup | Done |
| `/ready` | Fails until index + embeddings OK | Done |

**Gate:** 50× reconnect without orphan processes or stale locks — ✅ scripted equivalent passed (2026-06-08, `.\scripts\smoke\run-phase3.ps1 -Reconnects 50`); CI green on push — ✅ (2026-06-08).

---

## Phase 4 — Local ONNX + Docker ✅

**Goal:** Offline default profile; container release. Deliverables and gate evidence: [PHASE4_RESULTS.md](phases/PHASE4_RESULTS.md).

| Task | Criteria | Status |
|------|----------|--------|
| `internal/embeddings/onnx` | onnxruntime_go + bundled model | Done |
| Docker multi-stage | GHCR image | Done |
| Release workflow | Windows/Linux/macOS binaries with zvec vendor libs | Done |
| Install scripts | Production binary + optional model fetch | Done |

**Gate:** `local_multilingual` profile works without external API — ✅ (`scripts/smoke/run-phase4.*`, Docker offline config).

---

## Phase 5 — Shared daemon + v1.0 ✅

**Goal:** One HTTP service, multiple projects; hardening. Deliverables and gate evidence: [PHASE5_RESULTS.md](phases/PHASE5_RESULTS.md).

| Task | Criteria | Status |
|------|----------|--------|
| `internal/daemon` | WorkspaceRegistry, `daemon.yaml` | Done |
| HTTP `workspace_id` | All v1 routes multi-tenant | Done |
| MCP `--stdio-proxy` | Cursor → shared daemon | Done |
| `GET /v1/workspaces` | List registered workspaces | Done |
| Load tests, API auth | v1.0.0 tag | Done |

**Gate:** 3 workspaces on one daemon; MCP proxy from 2 Cursor projects — ✅ (`scripts/smoke/run-phase5.*`).

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
