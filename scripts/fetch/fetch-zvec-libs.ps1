# Clone zvec-ai/zvec-go v0.5.0 and download pre-built vendor libs into .deps/zvec-go.
$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$Dest = Join-Path $RepoRoot ".deps\zvec-go"
$Tag = if ($env:ZVEC_GO_TAG) { $env:ZVEC_GO_TAG } else { "v0.5.0" }

if (-not (Test-Path (Join-Path $Dest ".git"))) {
    New-Item -ItemType Directory -Force -Path (Split-Path $Dest) | Out-Null
    Write-Host "Cloning zvec-go $Tag into $Dest..."
    git clone --depth 1 --branch $Tag https://github.com/zvec-ai/zvec-go $Dest
} else {
    Push-Location $Dest
    try {
        $currentTag = git describe --tags --exact-match 2>$null
        if ($LASTEXITCODE -ne 0) { $currentTag = "unknown" }
        if ($currentTag -ne $Tag) {
            Write-Host "Updating zvec-go in $Dest to $Tag (was $currentTag)..."
            git fetch --depth 1 origin "tag $Tag"
            if ($LASTEXITCODE -ne 0) { throw "git fetch tag $Tag failed" }
            git checkout $Tag
            if ($LASTEXITCODE -ne 0) { throw "git checkout $Tag failed" }
        }
    } finally {
        Pop-Location
    }
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
