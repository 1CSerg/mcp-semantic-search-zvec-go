# zvec-go spike (Phase 1 gate)

Phase 1 must not proceed until this checklist passes on **Windows amd64** and **Linux amd64**.

## Prerequisites

```bash
git clone https://github.com/danieleugenewilliams/zvec-go
cd zvec-go
make deps   # per upstream README
```

Set `CGO_ENABLED=1`.

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

Align with `internal/store/zvec` (see [ARCHITECTURE.md](ARCHITECTURE.md)):

```
Collection name: ws_<sha256(workspace:profile:dims)[:16]>

Scalar fields:
  path, start_line, end_line, chunk_type, name, snippet

Vector field:
  embedding — FP32, dimensions = active profile dimensions
```

## Spike location

Implement POC in branch `phase1/zvec-spike`:

- `internal/store/zvec/store.go` — production wrapper
- `internal/store/zvec/store_test.go` — integration tests (build tag `integration`)

## Go / no-go

| Outcome | Action |
|---------|--------|
| All pass | Proceed Phase 1 |
| Windows link fails | Document MSVC steps; consider vendoring zvec libs |
| Schema incompatible | Document migration; indexes require reindex |
| zvec-go unmaintained | Fork or contribute upstream; track Alibaba official Go SDK |

## References

- [zvec-go](https://github.com/danieleugenewilliams/zvec-go)
- [Alibaba zvec](https://github.com/alibaba/zvec)
- [ROADMAP.md](ROADMAP.md) Phase 1
