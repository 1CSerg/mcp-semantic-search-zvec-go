#!/usr/bin/env bash
# Download ONNX Runtime shared library into .deps/onnxruntime/
# Prints ORT_LIB_DIR=... for eval or CI.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VERSION="${ONNXRUNTIME_VERSION:-1.26.0}"
DEST="$ROOT/.deps/onnxruntime"
mkdir -p "$DEST"

case "$(uname -s)/$(uname -m)" in
  Linux/x86_64)
    ARCHIVE="onnxruntime-linux-x64-${VERSION}.tgz"
    LIB_NAME="libonnxruntime.so"
    ;;
  Linux/aarch64|Linux/arm64)
    ARCHIVE="onnxruntime-linux-aarch64-${VERSION}.tgz"
    LIB_NAME="libonnxruntime.so"
    ;;
  Darwin/arm64)
    ARCHIVE="onnxruntime-osx-arm64-${VERSION}.tgz"
    LIB_NAME="libonnxruntime.dylib"
    ;;
  Darwin/x86_64)
    ARCHIVE="onnxruntime-osx-x86_64-${VERSION}.tgz"
    LIB_NAME="libonnxruntime.dylib"
    ;;
  MINGW*|MSYS*|CYGWIN*|Windows*)
    ARCHIVE="onnxruntime-win-x64-${VERSION}.zip"
    LIB_NAME="onnxruntime.dll"
    ;;
  *)
    echo "fetch-onnx-runtime: unsupported platform $(uname -s)/$(uname -m)" >&2
    exit 1
    ;;
esac

URL="https://github.com/microsoft/onnxruntime/releases/download/v${VERSION}/${ARCHIVE}"
TMP="$DEST/$ARCHIVE"

if [[ ! -f "$TMP" ]]; then
  echo "Downloading $URL..." >&2
  curl -fsSL "$URL" -o "$TMP"
fi

EXTRACT="$DEST/extract-${VERSION}"
rm -rf "$EXTRACT"
mkdir -p "$EXTRACT"

case "$ARCHIVE" in
  *.zip) unzip -q -o "$TMP" -d "$EXTRACT" ;;
  *.tgz) tar -xzf "$TMP" -C "$EXTRACT" ;;
esac

LIB_PATH="$(find "$EXTRACT" -name "$LIB_NAME" | head -n 1)"
if [[ -z "$LIB_PATH" ]]; then
  echo "fetch-onnx-runtime: $LIB_NAME not found in archive" >&2
  exit 1
fi

ORT_LIB_DIR="$DEST/$LIB_NAME.dir"
mkdir -p "$ORT_LIB_DIR"
cp -f "$LIB_PATH" "$ORT_LIB_DIR/$LIB_NAME"

# shellcheck disable=SC1091
source "$ROOT/scripts/dev/native-path.sh"
ORT_LIB_DIR="$(native_path "$ORT_LIB_DIR")"
ORT_LIB_PATH="$(native_path "$ORT_LIB_DIR/$LIB_NAME")"

printf 'ORT_LIB_DIR=%s\n' "$ORT_LIB_DIR" > "$ROOT/.deps/onnxruntime.env"
printf 'ONNXRUNTIME_SHARED_LIBRARY_PATH=%s\n' "$ORT_LIB_PATH" >> "$ROOT/.deps/onnxruntime.env"
echo "ORT_LIB_DIR=$ORT_LIB_DIR"
echo "ONNXRUNTIME_SHARED_LIBRARY_PATH=$ORT_LIB_PATH"
