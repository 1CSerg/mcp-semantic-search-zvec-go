# Clone zvec-ai/zvec-go v0.5.0 and download pre-built vendor libs into .deps/zvec-go.
$ErrorActionPreference = "Stop"
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Stay-OpenOnError.ps1')
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Invoke-RemoteFile.ps1')
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$Dest = Join-Path $RepoRoot ".deps\zvec-go"
$Tag = if ($env:ZVEC_GO_TAG) { $env:ZVEC_GO_TAG } else { "v0.5.0" }

function Get-NormalizedZvecTag {
    param([string]$Version)
    $Version = $Version.Trim()
    if (-not $Version.StartsWith('v')) { $Version = "v$Version" }
    return $Version
}

function Install-ZvecLibsWindows {
    param(
        [string]$Version,
        [string]$DestLib
    )
    $Version = Get-NormalizedZvecTag $Version
    $Artifact = "zvec-libs-windows-x64.zip"
    $Url = "https://github.com/zvec-ai/zvec-go/releases/download/$Version/$Artifact"
    New-Item -ItemType Directory -Force -Path $DestLib | Out-Null
    $Tmp = Join-Path $DestLib $Artifact

    Write-Host "Downloading pre-built libraries for windows/amd64 ($Version)..."
    Write-Host "  URL: $Url"
    Write-Host "  Destination: $DestLib"

    Invoke-RemoteFile -Uri $Url -OutFile $Tmp

    Write-Host "Extracting libraries..."
    Expand-Archive -Path $Tmp -DestinationPath $DestLib -Force
}

$failed = $false
try {

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

$LibDir = Join-Path $Dest "lib\windows_amd64"
$LibMarker = Join-Path $LibDir "zvec_c_api.dll"
if (-not (Test-Path $LibMarker)) {
    Push-Location $Dest
    try {
        if ($IsWindows -or $env:OS -eq 'Windows_NT') {
            Install-ZvecLibsWindows -Version $Tag -DestLib (Join-Path $Dest "lib")
        } else {
            go run ./cmd/download-libs -version $Tag 2>&1 | Write-Host
            if ($LASTEXITCODE -ne 0) { throw "download-libs failed with exit code $LASTEXITCODE" }
        }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "zvec libs already present at $LibDir"
}

if (-not (Test-Path $LibDir)) {
    throw "Expected lib dir missing: $LibDir"
}

$env:ZVEC_LIB_DIR = $LibDir
Write-Host "ZVEC_LIB_DIR=$LibDir"
$envFile = Join-Path $RepoRoot ".deps\zvec-lib.env"
New-Item -ItemType Directory -Force -Path (Split-Path $envFile) | Out-Null
Set-Content -Path $envFile -Value "ZVEC_LIB_DIR=$LibDir" -NoNewline -Encoding utf8

} catch {
    $failed = $true
    Write-Error $_
} finally {
    if ($failed) { Wait-IfInteractiveOnError }
}
if ($failed) { exit 1 }
