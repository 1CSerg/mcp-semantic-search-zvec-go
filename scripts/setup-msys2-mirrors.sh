#!/usr/bin/env bash
# Use BFSU mirror when msys2.org is slow (China-friendly; often faster globally too).
set -euo pipefail
MIRROR="https://mirrors.bfsu.edu.cn/msys2"
for f in /etc/pacman.d/mirrorlist /etc/pacman.d/mirrorlist.mingw /etc/pacman.d/mirrorlist.ucrt64 /etc/pacman.d/mirrorlist.clang64; do
  if [[ -f "$f" ]]; then
    sed -i "s|https://mirror.msys2.org|$MIRROR|g; s|https://repo.msys2.org|$MIRROR|g" "$f" 2>/dev/null || true
    echo "Server = $MIRROR/\$repo/os/\$arch" > "$f"
  fi
done
echo "Mirrors updated to $MIRROR"
