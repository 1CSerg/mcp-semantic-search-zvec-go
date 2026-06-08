# Phase 4 Results

## Scope delivered

- `internal/embeddings` — shared `Embedder` interface and provider factory
- `internal/embeddings/onnx` — local ONNX inference via `onnxruntime_go` + HuggingFace tokenizer (`build tag: onnx`)
- `internal/embeddings/onnx/paths.go` — model bundle resolution and validation (`model_optimized.onnx`, `tokenizer.json`)
- `scripts/fetch-onnx-model.*` — download default `paraphrase-multilingual-MiniLM-L12-v2` bundle
- `scripts/fetch-onnx-runtime.*` — download ONNX Runtime shared libraries into `.deps/onnxruntime/`
- `scripts/smoke-phase4.*` — gate smoke without external embedding API
- Production builds use `-tags "zvec,onnx"` (`Makefile`, `install.*`, `build-zvec-windows.ps1`)
- `docker/Dockerfile` — multi-stage image with zvec, ONNX runtime, bundled model, offline default config
- `.github/workflows/release.yml` — native CGO binaries + runtime libs per OS/arch

## Gate

| Check | Command | Result |
|-------|---------|--------|
| Unit tests | `go test ./...` | Pass |
| Coverage ≥88% `./internal/...`, ≥50% per package | `make test-cover-check` | Pass |
| Phase 4 smoke (Windows) | `.\scripts\smoke-phase4.ps1` | Pass (2026-06-08) |
| Docker image | Not run locally | Dockerfile updated for bundled ONNX/zvec runtime (`/etc/mcp-semantic-search-zvec-go/config.yaml`, `/opt/models/...`) |

Smoke validates:

1. `active_profile: local_multilingual` with no mock/cloud embedding server
2. `reindex` + `/ready`
3. `semantic_search` returns ranked results with `performance` metrics
4. `index_status.embedding_provider == onnx`

## Offline profile

Set in target project `config.yaml`:

```yaml
active_profile: local_multilingual
```

Model bundle path (relative to workspace):

`.mcp-semantic-search-zvec-go/models/paraphrase-multilingual-MiniLM-L12-v2/`

Required files:

- `model_optimized.onnx`
- `tokenizer.json`

Download once:

```powershell
.\scripts\fetch-onnx-model.ps1 -DestDir .\.mcp-semantic-search-zvec-go\models\paraphrase-multilingual-MiniLM-L12-v2
```

```bash
bash scripts/fetch-onnx-model.sh .mcp-semantic-search-zvec-go/models/paraphrase-multilingual-MiniLM-L12-v2
```

Then `reindex` with `force: true`.

## Known limitations

- Default `go test ./...` uses stub ONNX factory (`!onnx` tag); production binary requires `-tags "zvec,onnx"`.
- ONNX Runtime version pinned to `1.26.0` in fetch scripts; override with `ONNXRUNTIME_VERSION`.
- Model checksum env vars (`ONNX_MODEL_SHA256`, `ONNX_TOKENIZER_SHA256`) are optional; set for strict verification.
- Docker bind-mount workspaces still need polling file watcher on Windows hosts.
