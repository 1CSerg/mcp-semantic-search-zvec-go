# Production release build (same flags as .github/workflows/release.yml).
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Stay-OpenOnError.ps1')

$failed = $false
try {
    & "$PSScriptRoot\build-zvec-windows.ps1" -Release
    if ($LASTEXITCODE -ne 0) { throw "build-zvec-windows.ps1 failed with exit code $LASTEXITCODE" }
} catch {
    $failed = $true
    Write-Error $_
} finally {
    if ($failed) { Wait-IfInteractiveOnError }
}
if ($failed) { exit 1 }
