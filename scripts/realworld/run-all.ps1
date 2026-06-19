# Run realworld manual E2E scenarios (not CI).
param(
    [ValidateSet("onnx", "lmstudio")]
    [string]$Profile = "onnx",
    [switch]$KeepIndex,
    [string]$Run = "",
    [switch]$Docker
)

$ErrorActionPreference = "Stop"
$ScriptDir = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path

$setupArgs = @("-Profile", $Profile)
if ($KeepIndex) { $setupArgs += "-KeepIndex" }
& (Join-Path $ScriptDir "setup-harness.ps1") @setupArgs

if ($Profile -eq "lmstudio") {
    try {
        Invoke-RestMethod -Uri "http://127.0.0.1:1234/v1/models" -TimeoutSec 3 | Out-Null
    } catch {
        Write-Host "SKIP: LM Studio not reachable at http://127.0.0.1:1234"
        exit 0
    }
}

$zvecEnv = Join-Path $RepoRoot ".deps\zvec-lib.env"
if (-not (Test-Path $zvecEnv)) {
    & "$RepoRoot\scripts\fetch\fetch-zvec-libs.ps1" | Out-Null
} else {
    Get-Content $zvecEnv | ForEach-Object {
        if ($_ -match '^ZVEC_LIB_DIR=(.+)$') { $env:ZVEC_LIB_DIR = $Matches[1].Trim() }
    }
}
if (-not (Test-Path (Join-Path $RepoRoot ".deps\onnxruntime.env"))) {
    & "$RepoRoot\scripts\fetch\fetch-onnx-runtime.ps1" | Out-Null
} else {
    Get-Content (Join-Path $RepoRoot ".deps\onnxruntime.env") | ForEach-Object {
        if ($_ -match '^ORT_LIB_DIR=(.+)$') { $env:ORT_LIB_DIR = $Matches[1].Trim() }
    }
}
$env:CGO_ENABLED = "1"
if ($env:ZVEC_LIB_DIR) { $env:Path = "$($env:ZVEC_LIB_DIR);$($env:ORT_LIB_DIR);" + $env:Path }
$env:REALWORLD_REPO_ROOT = $RepoRoot
$env:REALWORLD_PROFILE = $Profile

$testArgs = @("-tags", "realworld,zvec", "-count=1", "-timeout", "30m", "-v", "./tests/realworld/...")
if ($Run) { $testArgs = @("-run", $Run) + $testArgs }

Push-Location $RepoRoot
try {
    go test @testArgs
    if ($Docker) {
        & (Join-Path $ScriptDir "run-docker.ps1")
    }
} finally {
    Pop-Location
}
