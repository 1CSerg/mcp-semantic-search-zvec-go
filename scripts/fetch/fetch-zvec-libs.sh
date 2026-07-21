#!/usr/bin/env bash
# Clone zvec-ai/zvec-go v0.6.0 into .deps/ and download pre-built native libs.
# Prints ZVEC_LIB_DIR=... for CI (append >> $GITHUB_ENV) or: eval "$(bash scripts/fetch/fetch-zvec-libs.sh)"
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEST="$ROOT/.deps/zvec-go"
TAG="${ZVEC_GO_TAG:-v0.6.0}"
ACP_PATCH_DIR="$ROOT/scripts/fetch/patches/zvec-go-acp"

apply_zvec_acp_patch() {
  local dest="$1"
  if [[ ! -d "$ACP_PATCH_DIR" ]]; then
    echo "fetch-zvec-libs: ACP patch dir missing: $ACP_PATCH_DIR" >&2
    exit 1
  fi
  if ! grep -q 'cStringPath' "$dest/collection.go"; then
    echo "Applying zvec-go ACP Unicode path patch..." >&2
    (cd "$dest" && git apply "$ACP_PATCH_DIR/collection.go.patch")
  fi
  local f
  for f in cpath.go path_unix.go path_windows.go path_windows_test.go path_windows_test_helper.go path_test.go collection_cyrillic_integration_test.go; do
    cp -f "$ACP_PATCH_DIR/$f" "$dest/$f"
  done
}

if [[ ! -d "$DEST/.git" ]]; then
  mkdir -p "$(dirname "$DEST")"
  echo "Cloning zvec-go $TAG into $DEST..." >&2
  git clone --depth 1 --branch "$TAG" https://github.com/zvec-ai/zvec-go "$DEST"
else
  current_tag="$(cd "$DEST" && git describe --tags --exact-match 2>/dev/null || true)"
  if [[ "$current_tag" != "$TAG" ]]; then
    echo "Updating zvec-go in $DEST to $TAG (was ${current_tag:-unknown})..." >&2
    (cd "$DEST" && git fetch --depth 1 origin tag "$TAG" && git checkout -f "$TAG" && git clean -fd --exclude=lib)
  fi
fi

apply_zvec_acp_patch "$DEST"

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

# shellcheck disable=SC1091
source "$ROOT/scripts/dev/native-path.sh"
ZVEC_LIB_DIR="$(native_path "$ZVEC_LIB_DIR")"

mkdir -p "$ROOT/.deps"
printf 'ZVEC_LIB_DIR=%s\n' "$ZVEC_LIB_DIR" > "$ROOT/.deps/zvec-lib.env"
echo "ZVEC_LIB_DIR=$ZVEC_LIB_DIR"
