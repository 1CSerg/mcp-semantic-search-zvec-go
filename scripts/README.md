# Scripts

Purpose-based layout for install, dependency fetch, contributor tooling, phase gate smokes, and optional spike helpers.

| Folder | Purpose | Examples |
|--------|---------|----------|
| **install/** | Wire MCP into a target project | `install.sh`, `install.ps1`, `uninstall.ps1` |
| **fetch/** | One-time native deps (zvec, ONNX runtime, model bundle) | `fetch-zvec-libs.*`, `fetch-onnx-runtime.*`, `fetch-onnx-model.*` |
| **dev/** | Contributors: hooks, coverage, Windows zvec build | `setup-git-hooks.*`, `check-coverage.*`, `git-add.sh`, `build-zvec-windows.ps1` |
| **smoke/** | Phase gate tests (`make smoke-phaseN`) | `run-phase1.*` … `run-phase5.*`, fixtures (`config.yaml`, `mock-embed.go`, …) |
| **spike/** | Docker zvec integration (optional manual check) | `run-docker.sh`, `run-docker-inner.sh` |
| **lib/** | Shared shell helpers | `normalize-eol.sh` |

## Common entry points

```bash
# Install into target project (from target project root)
TARGET_ROOT="$PWD" bash /path/to/clone/scripts/install/install.sh

# Fetch vendor libs (from repo root)
make fetch-zvec-libs
make fetch-onnx-runtime
make fetch-onnx-model

# Phase gate smokes (Linux/macOS)
make smoke-phase1   # … through smoke-phase5

# Coverage gate
make test-cover-check
```

```powershell
# Windows install
& "...\scripts\install\install.ps1" -TargetRoot (Get-Location).Path

# Windows build + smoke
.\scripts\dev\build-zvec-windows.ps1
.\scripts\smoke\run-phase5.ps1
.\scripts\dev\check-coverage.ps1
```

## Removed in v1.0 cleanup

Legacy zvec build scripts (`continue-zvec-build.sh`, `resume-build.sh`, `build-zvec-deps-windows.sh`, `reconfigure-zvec-mingw.sh`, `build-zvec-on-short-path.sh`), orphan `setup-msys2-mirrors.sh`, and duplicate `test-integration-docker.sh` were removed. Use `scripts/fetch/fetch-zvec-libs.*` instead.
