# zvec-go spike (Phase 1 gate)

Phase 1 must not proceed until this checklist passes on **Windows amd64** and **Linux amd64**.

## Prerequisites

```bash
make fetch-zvec-libs   # clone .deps/zvec-go + download vendor libs (v0.5.0)
export CGO_ENABLED=1
# Linux/macOS: source .deps/zvec-lib.env && export LD_LIBRARY_PATH="$ZVEC_LIB_DIR:$LD_LIBRARY_PATH"
```

Upstream: [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) (vendor mode, no `make deps`).

## Checklist

| # | Test | Pass criteria |
|---|------|---------------|
| 1 | Create collection with schema below | No CGO/link errors |
| 2 | Insert 100 docs with fp32 vectors | `doc_count` matches |
| 3 | Vector query top-k | Results ordered by score |
| 4 | Open existing collection read-only | Second open in same process idempotent |
| 5 | Delete by doc id | Count decreases |
| 6 | Graceful `Close()` then reopen | No LOCK file error |
| 7 | Kill -9 then stale lock handling | Next process reclaims (app-level) |
| 8 | Windows MSVC build | Binary runs on target host |

## Required schema

Align with `internal/store/zvec` (see [ARCHITECTURE.md](../ARCHITECTURE.md)):

```
Collection name: ws_<sha256(workspace:profile:dims)[:16]>

Scalar fields:
  path, start_line, end_line, chunk_type, name, snippet

Vector field:
  embedding — FP32, dimensions = active profile dimensions
```

## Spike location

- `internal/store/zvec/store.go` — production wrapper
- `internal/store/zvec/store_integration_test.go` — integration tests (build tag `integration,zvec`)

Run: `make test-integration`

## Go / no-go

| Outcome | Action |
|---------|--------|
| All pass | Proceed Phase 1 |
| Windows link fails | MSVC + `zvec_c_api.dll` next to exe; see [DEVELOPMENT.md](../DEVELOPMENT.md) |
| Schema incompatible | Document migration; indexes require reindex |
| Vendor libs GLIBC mismatch | Fallback: zvec-go source mode (`-tags source`) |

## References

- [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go)
- [zvec-go examples](https://github.com/zvec-ai/zvec-go/tree/main/examples)
- [Alibaba zvec](https://github.com/alibaba/zvec)
- [ROADMAP.md](../ROADMAP.md) Phase 1
- Build notes: [DEVELOPMENT.md](../DEVELOPMENT.md#zvec-go-phase-1)
