#!/usr/bin/env bash
# CRLF → LF для text-файлов. Согласовано с .gitattributes (eol=lf).
set -euo pipefail

normalize_eol_file() {
  local file="$1"
  [ -f "$file" ] || return 0

  if git check-attr binary -- "$file" | grep -q ': set$'; then
    return 0
  fi

  local before after
  before=$(wc -c <"$file" | tr -d '[:space:]')
  if sed --version >/dev/null 2>&1; then
    sed -i 's/\r$//' "$file"
  else
    sed -i '' 's/\r$//' "$file"
  fi
  after=$(wc -c <"$file" | tr -d '[:space:]')

  if [ "$before" != "$after" ]; then
    echo "normalize-eol: CRLF → LF: $file" >&2
  fi
}

normalize_eol_paths() {
  local -a paths=("$@")

  if [ ${#paths[@]} -eq 0 ]; then
    while IFS= read -r -d '' file; do
      normalize_eol_file "$file"
    done < <(git ls-files -z -c -o --exclude-standard)
    return 0
  fi

  local item
  for item in "${paths[@]}"; do
    if [ -f "$item" ]; then
      normalize_eol_file "$item"
    elif [ -d "$item" ]; then
      while IFS= read -r -d '' file; do
        normalize_eol_file "$file"
      done < <(find "$item" -type f ! -path '*/.git/*' -print0)
    fi
  done
}
