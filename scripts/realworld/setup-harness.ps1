# Setup ephemeral .realworld/ harness tree for manual E2E tests.
param(
    [ValidateSet("onnx", "lmstudio", "mock-fail", "mock-dim-mismatch")]
    [string]$Profile = "onnx",
    [switch]$KeepIndex
)

$ErrorActionPreference = "Stop"
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Stay-OpenOnError.ps1')
$ScriptDir = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$Realworld = Join-Path $RepoRoot ".realworld"
$BinDir = Join-Path $Realworld "bin"
$IndexDir = Join-Path $Realworld "data\index"
$ModelsDir = Join-Path $Realworld "models\paraphrase-multilingual-MiniLM-L12-v2"
$ConfigSrc = Join-Path $RepoRoot "tests\realworld\config\$Profile.yaml"
if (-not (Test-Path $ConfigSrc)) {
    $ConfigSrc = Join-Path $RepoRoot "tests\realworld\config\onnx.yaml"
}

function Copy-RuntimeLibs {
    param([string]$DestBinDir)
    $RepoBin = Join-Path $RepoRoot "bin"
    foreach ($name in @("zvec_c_api.dll", "onnxruntime.dll", "mcp-semantic-search-zvec-go.exe")) {
        $src = Join-Path $RepoBin $name
        if (Test-Path $src) {
            Copy-Item -Force $src (Join-Path $DestBinDir $name)
        }
    }
    if (-not (Test-Path (Join-Path $DestBinDir "zvec_c_api.dll"))) {
        & "$RepoRoot\scripts\fetch\fetch-zvec-libs.ps1" | Out-Null
        if ($env:ZVEC_LIB_DIR) {
            Copy-Item -Force (Join-Path $env:ZVEC_LIB_DIR "zvec_c_api.dll") (Join-Path $DestBinDir "zvec_c_api.dll") -ErrorAction SilentlyContinue
        }
    }
    if (-not (Test-Path (Join-Path $DestBinDir "onnxruntime.dll"))) {
        & "$RepoRoot\scripts\fetch\fetch-onnx-runtime.ps1" | Out-Null
        if ($env:ONNXRUNTIME_SHARED_LIBRARY_PATH) {
            Copy-Item -Force $env:ONNXRUNTIME_SHARED_LIBRARY_PATH (Join-Path $DestBinDir "onnxruntime.dll") -ErrorAction SilentlyContinue
        }
    }
}

$failed = $false
try {

Write-Host "==> build-zvec"
& "$RepoRoot\scripts\dev\build-zvec-windows.ps1" | Out-Null

if (-not $KeepIndex) {
    Write-Host "==> recreate $Realworld"
    if (Test-Path $Realworld) { Remove-Item -Recurse -Force $Realworld }
}

New-Item -ItemType Directory -Force -Path $BinDir, $IndexDir, (Join-Path $Realworld "logs"), (Join-Path $Realworld "models"), (Join-Path $Realworld "targets") | Out-Null

$BinName = "mcp-semantic-search-zvec-go.exe"
Copy-Item -Force (Join-Path $RepoRoot "bin\$BinName") (Join-Path $BinDir $BinName)
Copy-RuntimeLibs $BinDir

Copy-Item -Force $ConfigSrc (Join-Path $Realworld "config.yaml")
New-Item -ItemType File -Force -Path (Join-Path $Realworld ".env") | Out-Null
Set-Content -Path (Join-Path $BinDir "index-dir.txt") -Value $IndexDir -Encoding UTF8

if ($Profile -eq "onnx" -or (Select-String -Path (Join-Path $Realworld "config.yaml") -Pattern "active_profile:\s*local_multilingual" -Quiet)) {
    Write-Host "==> fetch ONNX model"
    & "$RepoRoot\scripts\fetch\fetch-onnx-model.ps1" -DestDir $ModelsDir | Out-Null
}

Write-Host "Harness ready: profile=$Profile"
Write-Host "  WORKSPACE_ROOT=$(Join-Path $RepoRoot 'tests\realworld\corpus')"
Write-Host "  INDEX_DIR=$IndexDir"
Write-Host "  CONFIG_PATH=$(Join-Path $Realworld 'config.yaml')"
Write-Host "  BIN=$(Join-Path $BinDir $BinName)"

} catch {
    $failed = $true
    Write-Error $_
} finally {
    if ($failed) { Wait-IfInteractiveOnError }
}
if ($failed) { exit 1 }
