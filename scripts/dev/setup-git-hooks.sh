#!/usr/bin/env bash
# Локальные настройки Git: git add без fatal на Windows + опциональный git stage.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

git config core.autocrlf false
git config core.safecrlf false
# git add и git stage — встроенные команды; addnorm — обёртка с CRLF→LF в рабочей копии.
git config alias.addnorm '!bash scripts/dev/git-add.sh'
git config --unset alias.stage 2>/dev/null || true

echo "Git: core.autocrlf=false, core.safecrlf=false, alias.addnorm=scripts/dev/git-add.sh"
