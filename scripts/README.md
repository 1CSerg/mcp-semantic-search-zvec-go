# Scripts

Purpose-based layout for install, dependency fetch, contributor tooling, phase gate smokes, and optional spike helpers.

| Folder | Purpose | Examples |
|--------|---------|----------|
| **install/** | Wire MCP into a target project | `install.sh`, `install.ps1`, `uninstall.ps1`, `merge-config.py` |
| **templates/** | MCP fragments, env example, Cursor agent rule | `cursor-mcp-*.json`, `cursor-rules/semantic-search-zvec-go.mdc`, `env.example` |
| **fetch/** | One-time native deps (zvec, ONNX runtime, model bundle, tree-sitter verify) | `fetch-zvec-libs.*`, `fetch-onnx-runtime.*`, `fetch-onnx-model.*`, `fetch-tree-sitter-grammars.*` |
| **dev/** | Contributors: hooks, coverage, Windows zvec build | `setup-git-hooks.*`, `check-coverage.*`, `git-add.sh`, `build-zvec-windows.ps1`, `build-release.*` |
| **smoke/** | Phase gate tests (`make smoke-phaseN`) | `run-phase1.*` … `run-phase5.*`, `run-phase5-docker.*`, `run-mcp-staging-multi-windows.ps1`, fixtures |
| **spike/** | Docker zvec integration (optional manual check) | `run-docker.sh`, `run-docker-inner.sh` |
| **lib/** | Shared shell helpers | `normalize-eol.sh` |

## Common entry points

```bash
# Install into target project (from target project root)
TARGET_ROOT="$PWD" bash /path/to/clone/scripts/install/install.sh
pip install -r /path/to/clone/scripts/install/requirements.txt   # once, for config merge on update

# Fetch vendor libs (from repo root)
make fetch-zvec-libs
make fetch-onnx-runtime
make fetch-onnx-model

# Verify tree-sitter CGO linkage (contributors; grammars via Go modules)
bash scripts/fetch/fetch-tree-sitter-grammars.sh
# Windows: .\scripts\fetch\fetch-tree-sitter-grammars.ps1

# Phase gate smokes (Linux/macOS)
make smoke-phase1   # … through smoke-phase5

# Coverage gate
make test-cover-check
```

```powershell
# Windows install
& "...\scripts\install\install.ps1" -TargetRoot (Get-Location).Path

# Windows build + smoke
.\scripts\dev\build-release.ps1   # production binary (-ldflags -s -w)
.\scripts\dev\build-zvec-windows.ps1
.\scripts\smoke\run-phase5.ps1
.\scripts\smoke\run-mcp-staging-multi-windows.ps1
.\scripts\smoke\run-phase5-docker.ps1
.\scripts\dev\check-coverage.ps1
```

## fetch-tree-sitter-grammars

`scripts/fetch/fetch-tree-sitter-grammars.sh` and `.ps1` verify that the repo compiles and links with build tag **`treesitter`** (CGO + `go-tree-sitter` / `tree-sitter-go` from Go modules). They do **not** download separate grammar archives — run after `make fetch-zvec-libs` when developing hybrid chunking. Full hybrid binary: `go build -tags "zvec,onnx,treesitter"` (not used by Release/install yet).

## Removed legacy scripts

Older zvec build helpers were removed; use `scripts/fetch/fetch-zvec-libs.*` instead.
