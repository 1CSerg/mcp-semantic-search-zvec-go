# Scripts

Purpose-based layout for install, dependency fetch, contributor tooling, phase gate smokes, and optional spike helpers.

| Folder | Purpose | Examples |
|--------|---------|----------|
| **install/** | Wire MCP into a target project | `install.sh`, `install.ps1`, `uninstall.ps1`, `merge-config.py` |
| **templates/** | MCP fragments, env example, Cursor agent rule | `cursor-mcp-*.json`, `cursor-rules/semantic-search-zvec-go.mdc`, `env.example` |
| **fetch/** | One-time native deps (zvec, ONNX runtime, model bundle, tree-sitter verify) | `fetch-zvec-libs.*`, `fetch-onnx-runtime.*`, `fetch-onnx-model.*`, `fetch-tree-sitter-grammars.*` |
| **dev/** | Contributors: hooks, coverage, Windows zvec build | `setup-git-hooks.*`, `check-coverage.*`, `git-add.sh`, `build-zvec-windows.ps1`, `build-release.*` |
| **smoke/** | Phase gate tests (`make smoke-phaseN`) | `run-phase1.*` … `run-phase5.*`, `run-phase5-docker.*`, `run-mcp-staging-multi-windows.ps1`, fixtures |
| **realworld/** | Manual E2E harness (`make test-realworld`) — **not CI** | `setup-harness.*`, `run-all.*`, `run-docker.*`; fixtures in `tests/realworld/` |
| **spike/** | Docker zvec integration (optional manual check) | `run-docker.sh`, `run-docker-inner.sh` |
| **lib/** | Shared shell helpers | `normalize-eol.sh`, `Stay-OpenOnError.ps1` (Windows: pause console on error), `Invoke-RemoteFile.ps1` (IWR + `curl.exe --ssl-no-revoke` fallback) |

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

# Realworld manual E2E (not CI; ONNX offline)
make test-realworld
make test-realworld-lmstudio   # needs LM Studio on :1234

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
.\scripts\realworld\run-all.ps1 -Profile onnx
.\scripts\realworld\run-all.ps1 -Profile onnx -Docker
```

Interactive Windows scripts pause on error (`Нажмите Enter для закрытия`) unless `STAY_OPEN_DISABLE=true`, `STAY_OPEN_SUPPRESS=1` (child scripts), or running in CI. Installed launchers ship `Stay-OpenOnError.ps1` in project `bin/`.

## Realworld harness

Extended manual E2E tests with a full multi-language corpus, MCP stdio subprocess, chunking assertions, and install-layout smoke. **Not** run in CI or pre-commit.

```bash
make test-realworld              # ONNX local_multilingual
make test-realworld-lmstudio     # lmstudio_qwen (skips if LM Studio down)
bash scripts/realworld/run-all.sh --profile onnx --run TestHTTP
bash scripts/realworld/run-all.sh --profile onnx --docker   # + Docker D1/D2
bash scripts/realworld/run-docker.sh                        # Docker only
```

Prerequisites: same as `make build-zvec` (CGO, zvec libs, ONNX runtime; ONNX model fetched by `setup-harness`). Runtime tree: `.realworld/` (gitignored). Wave 2 adds daemon/proxy, auth, concurrency, lifecycle, Docker smoke. Manual: T6 GUI, CLI5, D3 Docker+LM Studio. See [tests/realworld/README.md](../tests/realworld/README.md).

## fetch-tree-sitter-grammars

`scripts/fetch/fetch-tree-sitter-grammars.sh` and `.ps1` verify that the repo compiles and links with build tag **`treesitter`** (CGO + `go-tree-sitter` and grammars from Go modules: `tree-sitter-go`, `tree-sitter-python`, `tree-sitter-javascript`, `tree-sitter-typescript`, **`tree-sitter-bsl`**). They do **not** download separate grammar archives — run after `make fetch-zvec-libs` when developing hybrid chunking. Production binary uses `-tags "zvec,onnx,treesitter"` (Release, install, `make build-zvec`).

## Removed legacy scripts

Older zvec build helpers were removed; use `scripts/fetch/fetch-zvec-libs.*` instead.
