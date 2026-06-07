# Локальные настройки Git: git add без fatal на Windows + опциональный git stage.
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

git config core.autocrlf false
git config core.safecrlf false
# git add и git stage — встроенные команды; addnorm — обёртка с CRLF→LF в рабочей копии.
git config alias.addnorm '!bash scripts/git-add.sh'

# Убрать неработающие alias, если остались от прошлой настройки.
git config --unset alias.add 2>$null
git config --unset alias.stage 2>$null

Write-Host "Git: core.autocrlf=false, core.safecrlf=false, alias.addnorm=scripts/git-add.sh"
