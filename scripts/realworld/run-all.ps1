# Run realworld manual E2E scenarios (not CI).
param(
    [ValidateSet("onnx", "lmstudio")]
    [string]$Profile = "onnx",
    [switch]$KeepIndex,
    [string]$Run = "",
    [switch]$Docker
)

$ErrorActionPreference = "Stop"
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Stay-OpenOnError.ps1')
$ScriptDir = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path

$failed = $false
try {

$setupArgs = @{ Profile = $Profile }
if ($KeepIndex) { $setupArgs['KeepIndex'] = $true }
$prevStayOpenSuppress = $env:STAY_OPEN_SUPPRESS
$env:STAY_OPEN_SUPPRESS = '1'
try {
    & (Join-Path $ScriptDir "setup-harness.ps1") @setupArgs
    if ($LASTEXITCODE -ne 0) { throw "setup-harness.ps1 failed with exit code $LASTEXITCODE" }
} finally {
    if ($null -eq $prevStayOpenSuppress) {
        Remove-Item Env:STAY_OPEN_SUPPRESS -ErrorAction SilentlyContinue
    } else {
        $env:STAY_OPEN_SUPPRESS = $prevStayOpenSuppress
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
    if ($LASTEXITCODE -ne 0) { throw "go test failed with exit code $LASTEXITCODE" }
    if ($Docker) {
        & (Join-Path $ScriptDir "run-docker.ps1")
        if ($LASTEXITCODE -ne 0) { throw "run-docker.ps1 failed with exit code $LASTEXITCODE" }
    }
} finally {
    Pop-Location
}

} catch {
    $failed = $true
    Write-Error $_
} finally {
    if ($failed) { Wait-IfInteractiveOnError }
}
if ($failed) { exit 1 }
