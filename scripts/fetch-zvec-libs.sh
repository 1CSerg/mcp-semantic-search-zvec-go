#!/usr/bin/env bash
# Clone zvec-ai/zvec-go v0.3.1 into .deps/ and download pre-built native libs.
# Prints ZVEC_LIB_DIR=... for CI (append >> $GITHUB_ENV) or: eval "$(bash scripts/fetch-zvec-libs.sh)"
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/.deps/zvec-go"
TAG="${ZVEC_GO_TAG:-v0.3.1}"

if [[ ! -d "$DEST/.git" ]]; then
  mkdir -p "$(dirname "$DEST")"
  echo "Cloning zvec-go $TAG into $DEST..." >&2
  git clone --depth 1 --branch "$TAG" https://github.com/zvec-ai/zvec-go "$DEST"
fi

(cd "$DEST" && go run ./cmd/download-libs -version "$TAG" >&2)

lib_subdir=""
case "$(uname -s)/$(uname -m)" in
  Linux/x86_64)  lib_subdir="linux_amd64" ;;
  Linux/aarch64|Linux/arm64) lib_subdir="linux_arm64" ;;
  Darwin/arm64)  lib_subdir="darwin_arm64" ;;
  MINGW*|MSYS*|CYGWIN*|Windows*) lib_subdir="windows_amd64" ;;
  *)
    echo "fetch-zvec-libs: unsupported platform $(uname -s)/$(uname -m)" >&2
    exit 1
    ;;
esac

ZVEC_LIB_DIR="$DEST/lib/$lib_subdir"
if [[ ! -d "$ZVEC_LIB_DIR" ]]; then
  echo "fetch-zvec-libs: expected lib dir missing: $ZVEC_LIB_DIR" >&2
  exit 1
fi

mkdir -p "$ROOT/.deps"
printf 'ZVEC_LIB_DIR=%s\n' "$ZVEC_LIB_DIR" > "$ROOT/.deps/zvec-lib.env"
echo "ZVEC_LIB_DIR=$ZVEC_LIB_DIR"
