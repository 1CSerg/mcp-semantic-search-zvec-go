#!/usr/bin/env bash
# Обёртка git add: нормализует окончания строк до индексации.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

# shellcheck source=lib/normalize-eol.sh
source "$root/scripts/lib/normalize-eol.sh"

paths=()
for arg in "$@"; do
  case "$arg" in
    -*) ;;
    *) paths+=("$arg") ;;
  esac
done

normalize_eol_paths "${paths[@]}"

# Без alias.add, иначе рекурсия.
git -c alias.add= add "$@"
