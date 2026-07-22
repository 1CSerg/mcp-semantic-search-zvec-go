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
    (cd "$dest" && git apply --unidiff-zero "$ACP_PATCH_DIR/collection.go.patch")
  fi
  local f
  for f in cpath.go path_unix.go path_windows.go path_windows_test.go path_windows_test_helper_test.go path_test.go collection_cyrillic_integration_test.go; do
    cp -f "$ACP_PATCH_DIR/$f" "$dest/$f"
  done
}

verify_zvec_lib_sha256() {
  local lib_dir="$1"
  local lib_file=""
  if [[ -f "$lib_dir/zvec_c_api.dll" ]]; then
    lib_file="$lib_dir/zvec_c_api.dll"
  elif [[ -f "$lib_dir/libzvec_c_api.so" ]]; then
    lib_file="$lib_dir/libzvec_c_api.so"
  elif [[ -f "$lib_dir/libzvec_c_api.dylib" ]]; then
    lib_file="$lib_dir/libzvec_c_api.dylib"
  else
    return 1
  fi
  local sha_file="$lib_dir/.zvec-lib-sha256"
  [[ -f "$sha_file" ]] || return 1
  local want got
  want="$(tr -d '\r\n' < "$sha_file")"
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$lib_file" | awk '{print $1}')
  else
    got=$(shasum -a 256 "$lib_file" | awk '{print $1}')
  fi
  [[ "$got" == "$want" ]]
}

if [[ ! -d "$DEST/.git" ]]; then
  mkdir -p "$(dirname "$DEST")"
  echo "Cloning zvec-go $TAG into $DEST..." >&2
  git clone --depth 1 --branch "$TAG" https://github.com/zvec-ai/zvec-go "$DEST"
else
  current_tag="$(cd "$DEST" && git describe --tags --exact-match 2>/dev/null || true)"
  if [[ "$current_tag" != "$TAG" ]]; then
    echo "Updating zvec-go in $DEST to $TAG (was ${current_tag:-unknown})..." >&2
    (cd "$DEST" && git fetch --depth 1 origin tag "$TAG" && git checkout -f "$TAG" && git clean -fd)
  fi
fi

apply_zvec_acp_patch "$DEST"

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

LIB_DIR="$DEST/lib/$lib_subdir"
VERSION_MARKER="$LIB_DIR/.zvec-lib-version"
need_download=1
if [[ -f "$LIB_DIR/zvec_c_api.dll" || -f "$LIB_DIR/libzvec_c_api.so" || -f "$LIB_DIR/libzvec_c_api.dylib" ]] && [[ -f "$VERSION_MARKER" ]]; then
  marked_tag="$(tr -d '\r\n' < "$VERSION_MARKER")"
  if [[ "$marked_tag" == "$TAG" ]] && verify_zvec_lib_sha256 "$LIB_DIR"; then
    need_download=0
  elif [[ "$marked_tag" == "$TAG" ]]; then
    echo "zvec native lib SHA256 mismatch; refreshing lib..." >&2
    rm -rf "$DEST/lib"
  else
    echo "zvec native libs tag mismatch (marker=$marked_tag want=$TAG); refreshing lib..." >&2
    rm -rf "$DEST/lib"
  fi
fi
if [[ "$need_download" -eq 1 ]]; then
  (cd "$DEST" && go run ./cmd/download-libs -version "$TAG" >&2)
fi

ZVEC_LIB_DIR="$LIB_DIR"
if [[ ! -d "$ZVEC_LIB_DIR" ]]; then
  echo "fetch-zvec-libs: expected lib dir missing: $ZVEC_LIB_DIR" >&2
  exit 1
fi
printf '%s' "$TAG" > "$VERSION_MARKER"
if [[ -f "$ZVEC_LIB_DIR/zvec_c_api.dll" ]]; then
  sha256sum "$ZVEC_LIB_DIR/zvec_c_api.dll" | awk '{print $1}' > "$ZVEC_LIB_DIR/.zvec-lib-sha256"
elif [[ -f "$ZVEC_LIB_DIR/libzvec_c_api.so" ]]; then
  sha256sum "$ZVEC_LIB_DIR/libzvec_c_api.so" | awk '{print $1}' > "$ZVEC_LIB_DIR/.zvec-lib-sha256"
elif [[ -f "$ZVEC_LIB_DIR/libzvec_c_api.dylib" ]]; then
  shasum -a 256 "$ZVEC_LIB_DIR/libzvec_c_api.dylib" | awk '{print $1}' > "$ZVEC_LIB_DIR/.zvec-lib-sha256"
fi

# shellcheck disable=SC1091
source "$ROOT/scripts/dev/native-path.sh"
ZVEC_LIB_DIR="$(native_path "$ZVEC_LIB_DIR")"

mkdir -p "$ROOT/.deps"
printf 'ZVEC_LIB_DIR=%s\n' "$ZVEC_LIB_DIR" > "$ROOT/.deps/zvec-lib.env"
echo "ZVEC_LIB_DIR=$ZVEC_LIB_DIR"
