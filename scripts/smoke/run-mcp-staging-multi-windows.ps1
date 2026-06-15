# Smoke: two Windows project-local installs (native per-project) run in parallel with isolated workspaces.
$ErrorActionPreference = "Stop"
$ScriptDir = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$SmokeRoot = Join-Path $env:TEMP "mcp-zvec-smoke-staging-multi"
$HttpPortA = 18111
$HttpPortB = 18112
$MockPort = 9999
$Dims = 128
$ServerKey = "semantic-search-zvec-go"

function Stop-SmokeProcs {
    foreach ($p in $script:SmokeProcs) {
        if ($p -and -not $p.HasExited) {
            Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
        }
    }
}

function Normalize-WindowsPath {
    param([string]$Path)
    if ([string]::IsNullOrWhiteSpace($Path)) { return $Path }
    return ([System.IO.Path]::GetFullPath($Path)).Replace('/', '\')
}

function Get-ProjectBinExe {
    param([string]$Root)
    return Join-Path $Root ".mcp-semantic-search-zvec-go\bin\mcp-semantic-search-zvec-go.exe"
}

function Assert-McpJsonWindows {
    param(
        [string]$Root,
        [string]$ExpectedRoot
    )
    $mcpPath = Join-Path $Root ".cursor\mcp.json"
    if (-not (Test-Path $mcpPath)) { throw "missing mcp.json: $mcpPath" }

    $cfg = Get-Content -Raw $mcpPath | ConvertFrom-Json
    $srv = $cfg.mcpServers.$ServerKey
    if (-not $srv) { throw "missing mcp server key $ServerKey in $mcpPath" }

    if ([string]$srv.command -notmatch 'powershell\.exe$') {
        throw "expected powershell.exe command in mcp.json, got: $($srv.command)"
    }
    if ([string]$srv.command -match '/') { throw "forward slash in mcp.json command" }

    $fileArg = $false
    foreach ($arg in @($srv.args)) {
        if ($arg -eq "-File") { $fileArg = $true; continue }
        if ($fileArg) {
            if ([string]$arg -notmatch 'run-mcp-stdio\.ps1$') {
                throw "expected -File run-mcp-stdio.ps1, got: $arg"
            }
            if ([string]$arg -match '/') { throw "forward slash in launcher path: $arg" }
            $fileArg = $false
        }
    }

    $expectedRoot = Normalize-WindowsPath $ExpectedRoot
    foreach ($key in @("WORKSPACE_ROOT", "INDEX_DIR", "CONFIG_PATH")) {
        $val = [string]$srv.env.$key
        if ([string]::IsNullOrWhiteSpace($val)) { throw "missing env.$key in mcp.json" }
        if ($val -match '/') { throw "forward slash in env.$key" }
    }
    if ((Normalize-WindowsPath $srv.env.WORKSPACE_ROOT) -ne $expectedRoot) {
        throw "WORKSPACE_ROOT mismatch: $($srv.env.WORKSPACE_ROOT) != $expectedRoot"
    }
}

function Assert-CursorRuleInstalled {
    param([string]$Root)
    $rulePath = Join-Path $Root ".cursor\rules\semantic-search-zvec-go.mdc"
    if (-not (Test-Path $rulePath)) { throw "missing Cursor rule: $rulePath" }
    $content = Get-Content -Raw $rulePath
    if ($content -notmatch 'managedBy:\s*mcp-semantic-search-zvec-go') {
        throw "Cursor rule missing managedBy marker: $rulePath"
    }
}

function Assert-CursorRuleRemoved {
    param([string]$Root)
    $rulePath = Join-Path $Root ".cursor\rules\semantic-search-zvec-go.mdc"
    if (Test-Path $rulePath) { throw "Cursor rule still present after uninstall: $rulePath" }
}

function Wait-Health {
    param([int]$Port)
    $deadline = (Get-Date).AddSeconds(30)
    do {
        try {
            Invoke-RestMethod -Uri "http://127.0.0.1:$Port/health" -TimeoutSec 2 | Out-Null
            return
        } catch {
            if ((Get-Date) -gt $deadline) { throw "HTTP server did not become ready on :$Port" }
            Start-Sleep -Milliseconds 400
        }
    } while ($true)
}

function Wait-IndexingIdle {
    param([int]$Port)
    $deadline = (Get-Date).AddSeconds(180)
    do {
        $status = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/v1/status" -TimeoutSec 15
        if ($status.indexing.running) {
            if ($status.indexing.state -eq "error") {
                throw "indexing failed on :$Port : $($status.indexing.message)"
            }
        } else {
            if ($status.indexing.state -eq "error") {
                throw "indexing failed on :$Port : $($status.indexing.message)"
            }
            if ([int]$status.zvec_doc_count -ge 1 -or [int]$status.indexed_files -ge 1) {
                return $status
            }
        }
        if ((Get-Date) -gt $deadline) {
            throw "indexing did not produce docs on :$Port : $($status | ConvertTo-Json -Depth 4 -Compress)"
        }
        Start-Sleep -Milliseconds 800
    } while ($true)
}

function Start-SmokeHttpServer {
    param(
        [string]$BinExe,
        [string]$WorkspaceRoot,
        [int]$Port
    )
    $binDir = Split-Path $BinExe
    $root = Normalize-WindowsPath $WorkspaceRoot
    $installDir = Normalize-WindowsPath (Join-Path $root ".mcp-semantic-search-zvec-go")
    $pinfo = New-Object System.Diagnostics.ProcessStartInfo
    $pinfo.FileName = $BinExe
    $pinfo.Arguments = "--http --http-addr 127.0.0.1:$Port"
    $pinfo.WorkingDirectory = $binDir
    $pinfo.UseShellExecute = $false
    $pinfo.CreateNoWindow = $true
    $pinfo.EnvironmentVariables["WORKSPACE_ROOT"] = $root
    $pinfo.EnvironmentVariables["WORKSPACE_ID"] = $root
    $pinfo.EnvironmentVariables["INDEX_DIR"] = Normalize-WindowsPath (Join-Path $installDir "data\index")
    $pinfo.EnvironmentVariables["CONFIG_PATH"] = Normalize-WindowsPath (Join-Path $installDir "config.yaml")
    $pinfo.EnvironmentVariables["AUTO_INDEX_ON_START"] = "false"
    return [System.Diagnostics.Process]::Start($pinfo)
}

function New-SmokeProject {
    param(
        [string]$Root,
        [string]$Keyword,
        [string]$PkgName
    )
    New-Item -ItemType Directory -Force -Path (Join-Path $Root "pkg") | Out-Null
    Set-Content -Path (Join-Path $Root "pkg\$PkgName.go") -Value @"
package pkg

// $Keyword unique marker
func Handler() {}
"@ -Encoding ascii
}

$script:SmokeProcs = @()
try {
    Get-Process mcp-semantic-search-zvec-go -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Get-Process -Name "go" -ErrorAction SilentlyContinue | Where-Object {
        $_.Path -like "*\go.exe" -and $_.StartTime -gt (Get-Date).AddMinutes(-30)
    } | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 500
    if (Test-Path $SmokeRoot) { Remove-Item -Recurse -Force $SmokeRoot }
    New-Item -ItemType Directory -Force -Path $SmokeRoot | Out-Null

    $rootA = Join-Path $SmokeRoot "repo-alpha"
    $rootB = Join-Path $SmokeRoot "Тест репо beta"
    New-SmokeProject -Root $rootA -Keyword "alpha authentication gateway" -PkgName "alpha"
    New-SmokeProject -Root $rootB -Keyword "beta database repository layer" -PkgName "beta"

    if (-not (Test-Path (Join-Path $RepoRoot "bin\mcp-semantic-search-zvec-go.exe"))) {
        & "$RepoRoot\scripts\dev\build-zvec-windows.ps1" | Out-Null
    } else {
        & "$RepoRoot\scripts\dev\build-zvec-windows.ps1" | Out-Null
    }

    foreach ($root in @($rootA, $rootB)) {
        & "$RepoRoot\scripts\install\install.ps1" -TargetRoot $root -ReplaceConfig
        $cfgDest = Join-Path $root ".mcp-semantic-search-zvec-go\config.yaml"
        Copy-Item -Force (Join-Path $ScriptDir "config.yaml") $cfgDest
        $uninstall = Join-Path $root ".mcp-semantic-search-zvec-go\uninstall.ps1"
        if (-not (Test-Path $uninstall)) { throw "missing bundled uninstall.ps1: $uninstall" }
        Assert-CursorRuleInstalled -Root $root
    }

    $binA = Get-ProjectBinExe $rootA
    $binB = Get-ProjectBinExe $rootB
    if (-not (Test-Path $binA)) { throw "missing project binary A: $binA" }
    if (-not (Test-Path $binB)) { throw "missing project binary B: $binB" }
    if ($binA -eq $binB) { throw "project bin paths must differ for two projects" }

    Assert-McpJsonWindows -Root $rootA -ExpectedRoot (Resolve-Path $rootA).Path
    Assert-McpJsonWindows -Root $rootB -ExpectedRoot (Resolve-Path $rootB).Path

    foreach ($launcher in @(
            (Join-Path (Split-Path $binA) "run-mcp-stdio.ps1"),
            (Join-Path (Split-Path $binB) "run-mcp-stdio.ps1")
        )) {
        if (-not (Test-Path $launcher)) { throw "missing launcher: $launcher" }
    }

    $LibDir = $env:ZVEC_LIB_DIR
    if (-not $LibDir) {
        & "$RepoRoot\scripts\fetch\fetch-zvec-libs.ps1" | Out-Null
        $LibDir = $env:ZVEC_LIB_DIR
    }
    $env:CGO_ENABLED = "1"
    $env:Path = "$LibDir;" + $env:Path
    if (Test-Path "D:\tools\winlibs\mingw64\bin\gcc.exe") {
        $env:Path = "D:\tools\winlibs\mingw64\bin;" + $env:Path
        $env:CC = "gcc"
    }

    $mock = Start-Process -FilePath "go" -ArgumentList @(
        "run", "$ScriptDir\mock-embed.go", "-port", $MockPort, "-dims", $Dims
    ) -PassThru -WindowStyle Hidden
    $script:SmokeProcs += $mock
    Start-Sleep -Seconds 2

    $prevAuto = $env:AUTO_INDEX_ON_START
    $env:AUTO_INDEX_ON_START = "false"
    $srvA = Start-SmokeHttpServer -BinExe $binA -WorkspaceRoot (Resolve-Path $rootA).Path -Port $HttpPortA
    $srvB = Start-SmokeHttpServer -BinExe $binB -WorkspaceRoot (Resolve-Path $rootB).Path -Port $HttpPortB
    if ($null -ne $prevAuto) { $env:AUTO_INDEX_ON_START = $prevAuto } else { Remove-Item Env:AUTO_INDEX_ON_START -ErrorAction SilentlyContinue }
    $script:SmokeProcs += $srvA, $srvB

    Wait-Health $HttpPortA
    Wait-Health $HttpPortB
    Start-Sleep -Seconds 1

    foreach ($pair in @(
            @{ Port = $HttpPortA; Keyword = "alpha authentication gateway" },
            @{ Port = $HttpPortB; Keyword = "beta database repository layer" }
        )) {
        $body = @{ force = $true } | ConvertTo-Json
        $reindex = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$($pair.Port)/v1/reindex" `
            -ContentType "application/json" -Body $body
        if (-not $reindex.started) { throw "reindex not started on :$($pair.Port): $($reindex.message)" }
        Wait-IndexingIdle $pair.Port | Out-Null
    }

    $statusA = Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPortA/v1/status"
    $statusB = Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPortB/v1/status"
    $wsA = [string]$statusA.workspace_root
    $wsB = [string]$statusB.workspace_root
    if ($wsA -eq $wsB) { throw "both servers report same workspace_root: $wsA" }
    if ($wsA -notmatch "repo-alpha") { throw "unexpected workspace A: $wsA" }
    if ($wsB -notmatch "Тест") { throw "unexpected workspace B: $wsB" }

    $searchA = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPortA/v1/search" `
        -ContentType "application/json" -Body (@{ query = "alpha authentication gateway"; limit = 3 } | ConvertTo-Json)
    $searchB = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPortB/v1/search" `
        -ContentType "application/json" -Body (@{ query = "beta database repository layer"; limit = 3 } | ConvertTo-Json)
    if (@($searchA.results).Count -lt 1) { throw "no results on A" }
    if (@($searchB.results).Count -lt 1) { throw "no results on B" }

    $cross = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPortB/v1/search" `
        -ContentType "application/json" -Body (@{ query = "alpha authentication gateway"; limit = 3 } | ConvertTo-Json)
    foreach ($r in @($cross.results)) {
        if ([string]$r.snippet -match "alpha authentication") {
            throw "cross-workspace leak: B returned alpha snippet"
        }
    }

    Stop-Process -Id $srvA.Id -Force
    Start-Sleep -Milliseconds 500
    if ($srvB.HasExited) { throw "server B exited when A was stopped" }
    Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPortB/health" -TimeoutSec 5 | Out-Null

    Stop-Process -Id $srvB.Id -Force
    Start-Sleep -Milliseconds 500

    $launcherA = Join-Path (Split-Path $binA) "run-mcp-stdio.ps1"
    $stdioProc = Start-Process -FilePath "powershell.exe" -ArgumentList @(
        "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $launcherA
    ) -PassThru -WindowStyle Hidden
    $script:SmokeProcs += $stdioProc
    $stdioLock = Join-Path $rootA ".mcp-semantic-search-zvec-go\data\index\stdio.lock"
    $deadline = (Get-Date).AddSeconds(30)
    do {
        if (Test-Path $stdioLock) { break }
        if ($stdioProc.HasExited) { throw "stdio launcher exited before stdio.lock appeared" }
        if ((Get-Date) -gt $deadline) { throw "stdio.lock did not appear for $rootA" }
        Start-Sleep -Milliseconds 300
    } while ($true)

    $uninstallA = Join-Path $rootA ".mcp-semantic-search-zvec-go\uninstall.ps1"
    & $uninstallA
    if ($LASTEXITCODE -ne 0) { throw "uninstall.ps1 failed with exit code $LASTEXITCODE" }
    if (Test-Path (Join-Path $rootA ".mcp-semantic-search-zvec-go")) {
        throw "install dir still present after stdio uninstall: $rootA"
    }
    if (Test-Path $stdioLock) { throw "stdio.lock still present after uninstall" }
    $mcpAfter = Join-Path $rootA ".cursor\mcp.json"
    if (Test-Path $mcpAfter) {
        $cfgAfter = Get-Content -Raw $mcpAfter | ConvertFrom-Json
        if ($cfgAfter.mcpServers.PSObject.Properties.Name -contains $ServerKey) {
            throw "mcp.json still contains $ServerKey after uninstall"
        }
    }
    Assert-CursorRuleRemoved -Root $rootA

    Write-Host "PASS project-local multi-windows smoke: two installs, parallel HTTP, workspace isolation, stdio uninstall"
} finally {
    Stop-SmokeProcs
}
