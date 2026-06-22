# Локальные настройки Git: git add без fatal на Windows + опциональный git stage.
$ErrorActionPreference = "Stop"
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Stay-OpenOnError.ps1')

$failed = $false
try {
    $root = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
    Set-Location $root

    git config core.autocrlf false
    git config core.safecrlf false
    # git add и git stage — встроенные команды; addnorm — обёртка с CRLF→LF в рабочей копии.
    git config alias.addnorm '!bash scripts/dev/git-add.sh'

    # Убрать неработающие alias, если остались от прошлой настройки.
    git config --unset alias.add 2>$null
    git config --unset alias.stage 2>$null

    Write-Host "Git: core.autocrlf=false, core.safecrlf=false, alias.addnorm=scripts/dev/git-add.sh"
} catch {
    $failed = $true
    Write-Error $_
} finally {
    if ($failed) { Wait-IfInteractiveOnError }
}
if ($failed) { exit 1 }
