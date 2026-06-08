# Build mcp-semantic-search-zvec-go with zvec vendor libs (Windows MSVC/MinGW CGO).
$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path

& "$RepoRoot\scripts\fetch\fetch-zvec-libs.ps1" | Out-Null
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
    & "$RepoRoot\scripts\fetch\fetch-onnx-runtime.ps1" | Out-Null
    New-Item -ItemType Directory -Force -Path bin | Out-Null
    go build -tags "zvec,onnx" -o bin\mcp-semantic-search-zvec-go.exe .\cmd\mcp-semantic-search-zvec-go
    $dllDst = Join-Path $LibDir "zvec_c_api.dll"
    if (Test-Path $dllDst) {
        Copy-Item -Force $dllDst bin\ -ErrorAction SilentlyContinue
    }
    $ortDll = Join-Path $env:ORT_LIB_DIR "onnxruntime.dll"
    if (Test-Path $ortDll) {
        Copy-Item -Force $ortDll bin\ -ErrorAction SilentlyContinue
    }
    Write-Host "Built bin\mcp-semantic-search-zvec-go.exe (tags: zvec,onnx)"
} finally {
    Pop-Location
}
