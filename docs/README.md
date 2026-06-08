# Documentation

## Root (user and contributor reference)

| Document | Audience | Description |
|----------|----------|-------------|
| [INSTALL.md](INSTALL.md) | Target project | Install into a codebase, MCP wiring, secrets |
| [CONFIG.md](CONFIG.md) | Target project | `config.yaml`, env vars, profiles |
| [API.md](API.md) | Integrators | HTTP REST and MCP tools |
| [ARCHITECTURE.md](ARCHITECTURE.md) | All | Components, deployment modes, resilience |
| [DEVELOPMENT.md](DEVELOPMENT.md) | Repo contributors | Build, tests, CGO, smoke gates |
| [ROADMAP.md](ROADMAP.md) | All | Phases 0–5 status and gates |

Agent guide for MCP in a **target project** (not this repo): [AGENTS.md](../AGENTS.md).

## phases/

Gate evidence and deliverable notes for completed phases:

| Document | Phase |
|----------|-------|
| [PHASE3_RESULTS.md](phases/PHASE3_RESULTS.md) | Resilience, watcher, `/ready` |
| [PHASE4_RESULTS.md](phases/PHASE4_RESULTS.md) | Local ONNX, Docker release |
| [PHASE5_RESULTS.md](phases/PHASE5_RESULTS.md) | Shared daemon, MCP proxy, v1.0 |

## spike/

zvec-go integration checklist and run logs:

| Document | Purpose |
|----------|---------|
| [ZVEC_SPIKE.md](spike/ZVEC_SPIKE.md) | Phase 1 gate checklist (CGO, schema, integration tests) |
| [SPIKE_RESULTS.md](spike/SPIKE_RESULTS.md) | Recorded spike and smoke runs |
