$ErrorActionPreference = "Stop"
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Stay-OpenOnError.ps1')
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Invoke-RemoteFile.ps1')
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$Version = if ($env:ONNXRUNTIME_VERSION) { $env:ONNXRUNTIME_VERSION } else { "1.26.0" }
$Dest = Join-Path $RepoRoot ".deps\onnxruntime"

$failed = $false
try {
New-Item -ItemType Directory -Force -Path $Dest | Out-Null

$Archive = "onnxruntime-win-x64-$Version.zip"
$LibName = "onnxruntime.dll"
$Url = "https://github.com/microsoft/onnxruntime/releases/download/v$Version/$Archive"
$Tmp = Join-Path $Dest $Archive

if (-not (Test-Path $Tmp)) {
    Write-Host "Downloading $Url..."
    Invoke-RemoteFile -Uri $Url -OutFile $Tmp
}

$Extract = Join-Path $Dest "extract-$Version"
if (Test-Path $Extract) { Remove-Item -Recurse -Force $Extract }
Expand-Archive -Path $Tmp -DestinationPath $Extract -Force

$LibPath = Get-ChildItem -Path $Extract -Recurse -Filter $LibName | Select-Object -First 1
if (-not $LibPath) { throw "$LibName not found in archive" }

$OrtLibDir = Join-Path $Dest "onnxruntime.dll.dir"
New-Item -ItemType Directory -Force -Path $OrtLibDir | Out-Null
Copy-Item -Force $LibPath.FullName (Join-Path $OrtLibDir $LibName)

$envFile = Join-Path $RepoRoot ".deps\onnxruntime.env"
@(
    "ORT_LIB_DIR=$OrtLibDir"
    "ONNXRUNTIME_SHARED_LIBRARY_PATH=$OrtLibDir\$LibName"
) | Set-Content -Encoding ascii $envFile

$env:ORT_LIB_DIR = $OrtLibDir
$env:ONNXRUNTIME_SHARED_LIBRARY_PATH = Join-Path $OrtLibDir $LibName
Write-Host "ORT_LIB_DIR=$OrtLibDir"
Write-Host "ONNXRUNTIME_SHARED_LIBRARY_PATH=$($env:ONNXRUNTIME_SHARED_LIBRARY_PATH)"

} catch {
    $failed = $true
    Write-Error $_
} finally {
    if ($failed) { Wait-IfInteractiveOnError }
}
if ($failed) { exit 1 }
