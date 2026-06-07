# Clone zvec-ai/zvec-go and download pre-built vendor libs into .deps/zvec-go.
$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot
$Dest = Join-Path $RepoRoot ".deps\zvec-go"
$Tag = if ($env:ZVEC_GO_TAG) { $env:ZVEC_GO_TAG } else { "v0.3.1" }

if (-not (Test-Path (Join-Path $Dest ".git"))) {
    New-Item -ItemType Directory -Force -Path (Split-Path $Dest) | Out-Null
    git clone --depth 1 --branch $Tag https://github.com/zvec-ai/zvec-go $Dest
}

Push-Location $Dest
try {
    go run ./cmd/download-libs -version $Tag 2>&1 | Write-Host
} finally {
    Pop-Location
}

$LibDir = Join-Path $Dest "lib\windows_amd64"
if (-not (Test-Path $LibDir)) {
    throw "Expected lib dir missing: $LibDir"
}

$env:ZVEC_LIB_DIR = $LibDir
Write-Host "ZVEC_LIB_DIR=$LibDir"
$envFile = Join-Path $RepoRoot ".deps\zvec-lib.env"
New-Item -ItemType Directory -Force -Path (Split-Path $envFile) | Out-Null
Set-Content -Path $envFile -Value "ZVEC_LIB_DIR=$LibDir" -NoNewline -Encoding utf8
