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

foreach ($name in @('zvec-lib.env', 'onnxruntime.env')) {
	$file = Join-Path $RepoRoot (Join-Path '.deps' $name)
	if (-not (Test-Path $file)) { continue }
	Get-Content $file | ForEach-Object {
		if ($_ -match '^([^=]+)=(.*)$') {
			Set-Item -Path "Env:$($Matches[1])" -Value $Matches[2].Trim()
		}
	}
}

Add-NativeLibDir $env:ZVEC_LIB_DIR
Add-NativeLibDir $env:ORT_LIB_DIR
