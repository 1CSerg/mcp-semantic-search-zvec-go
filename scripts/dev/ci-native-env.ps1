# Load .deps/*.env and prepend native DLL dirs to PATH (Windows CI / local).
$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path

function Convert-MsysPath([string]$Path) {
	if ([string]::IsNullOrWhiteSpace($Path)) { return $Path }
	if ($Path -match '^/([a-zA-Z])/(.*)$') {
		return ($Matches[1] + ':\' + ($Matches[2] -replace '/', '\'))
	}
	return $Path
}

function Add-NativeLibDir([string]$Dir) {
	$native = Convert-MsysPath $Dir
	if ([string]::IsNullOrWhiteSpace($native)) { return }
	$env:PATH = "$native;$env:PATH"
}

# Bash consumers quote values that contain backslashes (see fetch-zvec-libs.sh),
# so strip surrounding quotes before using the value as a native path.
function ConvertFrom-EnvFileValue([string]$Value) {
	return $Value.Trim().Trim("'").Trim('"')
}

foreach ($name in @('zvec-lib.env', 'onnxruntime.env')) {
	$file = Join-Path $RepoRoot (Join-Path '.deps' $name)
	if (-not (Test-Path $file)) { continue }
	Get-Content $file | ForEach-Object {
		if ($_ -match '^([^=]+)=(.*)$') {
			Set-Item -Path "Env:$($Matches[1])" -Value (ConvertFrom-EnvFileValue $Matches[2])
		}
	}
}

Add-NativeLibDir $env:ZVEC_LIB_DIR
Add-NativeLibDir $env:ORT_LIB_DIR
