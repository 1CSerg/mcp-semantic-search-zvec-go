# Build mcp-semantic-search-zvec-go with zvec vendor libs (Windows MSVC/MinGW CGO).
$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot

& "$RepoRoot\scripts\fetch-zvec-libs.ps1" | Out-Null
$LibDir = $env:ZVEC_LIB_DIR

$env:CGO_ENABLED = "1"
$env:Path = "$LibDir;" + $env:Path

$mingw = "D:\tools\winlibs\mingw64\bin"
if (Test-Path "$mingw\gcc.exe") {
    $env:Path = "$mingw;" + $env:Path
    $env:CC = "gcc"
    $env:CXX = "g++"
} else {
    $vcvars = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvars64.bat"
    if (-not (Test-Path $vcvars)) {
        throw "No MinGW gcc and no VS Build Tools at $vcvars"
    }
    cmd /c "`"$vcvars`" && set" | ForEach-Object {
        if ($_ -match "^(.*?)=(.*)$") {
            Set-Item -Path "env:$($matches[1])" -Value $matches[2]
        }
    }
    $llvm = "${env:ProgramFiles}\Microsoft Visual Studio\2022\BuildTools\VC\Tools\Llvm\bin"
    if (Test-Path "$llvm\clang-cl.exe") {
        $env:Path = "$llvm;" + $env:Path
        $env:CC = "clang-cl"
        $env:CXX = "clang-cl"
    } else {
        $env:CC = "cl"
        $env:CXX = "cl"
    }
}

Push-Location $RepoRoot
try {
    New-Item -ItemType Directory -Force -Path bin | Out-Null
    go build -tags zvec -o bin\mcp-semantic-search-zvec-go.exe .\cmd\mcp-semantic-search-zvec-go
    Copy-Item -Force (Join-Path $LibDir "zvec_c_api.dll") bin\
    Write-Host "Built bin\mcp-semantic-search-zvec-go.exe"
} finally {
    Pop-Location
}
