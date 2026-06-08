#!/usr/bin/env bash
# Download default local_multilingual ONNX model bundle (tokenizer + model_optimized.onnx).
# Usage: bash scripts/fetch-onnx-model.sh [DEST_DIR]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${1:-$ROOT/.mcp-semantic-search-zvec-go/models/paraphrase-multilingual-MiniLM-L12-v2}"
MODEL_URL="${ONNX_MODEL_URL:-https://huggingface.co/Xenova/paraphrase-multilingual-MiniLM-L12-v2/resolve/main/onnx/model.onnx}"
TOKENIZER_URL="${ONNX_TOKENIZER_URL:-https://huggingface.co/Xenova/paraphrase-multilingual-MiniLM-L12-v2/resolve/main/tokenizer.json}"
MODEL_SHA256="${ONNX_MODEL_SHA256:-}"
TOKENIZER_SHA256="${ONNX_TOKENIZER_SHA256:-}"

mkdir -p "$DEST"

download() {
  local url="$1"
  local out="$2"
  echo "Downloading $(basename "$out")..." >&2
  curl -fsSL "$url" -o "$out"
}

verify_sha256() {
  local file="$1"
  local want="$2"
  if [[ -z "$want" ]]; then
    return 0
  fi
  local got
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$file" | awk '{print $1}')
  else
    got=$(shasum -a 256 "$file" | awk '{print $1}')
  fi
  if [[ "$got" != "$want" ]]; then
    echo "checksum mismatch for $file: got $got want $want" >&2
    exit 1
  fi
}

model_tmp="$DEST/model_optimized.onnx"
tokenizer_tmp="$DEST/tokenizer.json"

download "$MODEL_URL" "$model_tmp"
download "$TOKENIZER_URL" "$tokenizer_tmp"
verify_sha256 "$model_tmp" "$MODEL_SHA256"
verify_sha256 "$tokenizer_tmp" "$TOKENIZER_SHA256"

echo "ONNX model bundle ready at $DEST"
