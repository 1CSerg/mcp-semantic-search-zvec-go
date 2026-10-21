#!/usr/bin/env bash
# Shared guard: detect stale zvec native libraries left in the repository root.
#
# A zvec native library in the repo root silently wins the loader search order
# over the pinned copy in .deps/zvec-go/lib/<os>_<arch>, producing load failures
# ("cannot open shared object file", Windows "0xc0000139") that look like a
# broken build. Source this file and call warn_stale_zvec_root_native_libs.
#
# Related plan: docs/plans/2026-10-19-post-upgrade-followups.md (problem 2).

ZVEC_NATIVE_LIB_NAMES=(zvec_c_api.dll libzvec_c_api.so libzvec_c_api.dylib)

zvec_sha256() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  else
    shasum -a 256 "$file" | awk '{print $1}'
  fi
}

# Prints a warning (never fails the build) when stale native libs sit in the root.
warn_stale_zvec_root_native_libs() {
  local root="$1"
  local lib_dir="${2:-}"
  local pinned="" name path got reason found=0

  if [[ -n "$lib_dir" && -f "$lib_dir/.zvec-lib-sha256" ]]; then
    pinned="$(tr -d '\r\n' < "$lib_dir/.zvec-lib-sha256")"
  fi

  for name in "${ZVEC_NATIVE_LIB_NAMES[@]}"; do
    path="$root/$name"
    [[ -f "$path" ]] || continue
    reason="unpinned copy in the repository root shadows .deps/zvec-go/lib"
    if [[ -n "$pinned" ]]; then
      got="$(zvec_sha256 "$path")"
      if [[ "$got" == "$pinned" ]]; then
        reason="identical to the pinned copy, but still shadows the library search order"
      else
        reason="SHA256 ${got:0:12}... differs from pinned ${pinned:0:12}..."
      fi
    fi
    if [[ "$found" -eq 0 ]]; then
      echo "" >&2
      echo "WARNING: stale zvec native library in the repository root:" >&2
      found=1
    fi
    echo "  $path" >&2
    echo "    $reason" >&2
  done

  if [[ "$found" -eq 1 ]]; then
    echo "  The root copy is loaded before .deps/zvec-go/lib and causes load failures." >&2
    echo "  Remove it: rm -f $root/zvec_c_api.dll $root/libzvec_c_api.so $root/libzvec_c_api.dylib" >&2
    echo "  Binaries are reproducible with scripts/fetch/fetch-zvec-libs.sh." >&2
    echo "" >&2
  fi
}
