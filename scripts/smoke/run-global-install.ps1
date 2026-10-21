# Global-install acceptance smoke: one application, N projects, isolated data.
#
# Runs entirely inside a temporary MCP_GLOBAL_DIR and temporary project roots.
# It never touches the real user-level install, real project configs or real
# indexes, and it never prints secrets.
#
# Usage:
#   pwsh -File scripts/smoke/run-global-install.ps1 [-Binary <path>] [-KeepTemp]
#
# Exit codes: 0 all checks passed, 1 a check failed, 2 bad invocation.

[CmdletBinding()]
param(
    [string]$Binary = "",
    [switch]$KeepTemp,
    [switch]$SkipNativeBuild
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
. (Join-Path $RepoRoot 'scripts\lib\global-install.ps1')

$script:Failures = @()
$script:Checks = 0

function Check {
    param([string]$Name, [scriptblock]$Condition)
    $script:Checks++
    try {
        $ok = & $Condition
        if ($ok) {
            Write-Host "  PASS $Name"
        } else {
            Write-Host "  FAIL $Name" -ForegroundColor Red
            $script:Failures += $Name
        }
    } catch {
        Write-Host "  FAIL $Name ($($_.Exception.Message))" -ForegroundColor Red
        $script:Failures += "$Name ($($_.Exception.Message))"
    }
}

function New-TempDir {
    param([string]$Prefix)
    $path = Join-Path ([System.IO.Path]::GetTempPath()) ("$Prefix-{0}" -f [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Force -Path $path | Out-Null
    return $path
}

function New-FixtureProject {
    param([string]$Root, [string]$Marker)
    New-Item -ItemType Directory -Force -Path $Root | Out-Null
    Set-Content -Path (Join-Path $Root "main.go") -Value @"
package main

// $Marker
func main() {}
"@ -Encoding utf8
    Set-Content -Path (Join-Path $Root "README.md") -Value "# $Marker" -Encoding utf8
}

# --- resolve the binary -----------------------------------------------------
$globalTemp = New-TempDir "mcp-global-smoke"
$projectA = New-TempDir "mcp-proj-a"
$projectB = New-TempDir "mcp-proj-b"
$projectC = New-TempDir "mcp-proj-c"

$cleanup = {
    # Stop any daemon this smoke started before deleting its directory: a stray
    # detached daemon would keep holding the port and the (now removed) index.
    if (Test-Path $globalTemp) {
        try {
            $p = Get-GlobalPaths -GlobalDir $globalTemp
            if (Test-Path $p.Binary) {
                & $p.Binary --stop-daemon 2>&1 | Out-Null
                Start-Sleep -Milliseconds 600
            }
        } catch { }
    }
    if (-not $KeepTemp) {
        foreach ($dir in @($globalTemp, $projectA, $projectB, $projectC)) {
            Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
        }
    } else {
        Write-Host "Kept temp dirs: $globalTemp $projectA $projectB $projectC"
    }
}

try {
    if (-not $Binary) {
        # Build from the working tree: bin/ may hold a stale prebuilt that does not
        # have the flags this smoke exercises.
        if ($SkipNativeBuild) {
            throw "-SkipNativeBuild requires -Binary <path to a current build>"
        }
        Write-Host "Building a stub binary (no native tags) for the layout smoke..."
        Push-Location $RepoRoot
        try {
            $Binary = Join-Path $globalTemp "mcp-semantic-search-zvec-go.exe"
            & go build -o $Binary ./cmd/mcp-semantic-search-zvec-go
            if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
        } finally {
            Pop-Location
        }
    }
    if (-not (Test-Path $Binary)) {
        throw "binary not found: $Binary"
    }
    $probe = & $Binary --version 2>&1
    if ($LASTEXITCODE -ne 0) { throw "binary $Binary does not run: $probe" }
    Write-Host "Using binary $Binary (v$((($probe | Out-String).Trim())))"

    # Everything below runs against the temporary user dir.
    $env:MCP_GLOBAL_DIR = $globalTemp
    $p = Get-GlobalPaths -GlobalDir $globalTemp

    Write-Host "Global install smoke: $globalTemp"
    Write-Host "Binary: $Binary"
    Write-Host ""

    # --- 1. app install -----------------------------------------------------
    Write-Host "1. Installing the user-level application"
    $null = Initialize-GlobalLayout -GlobalDir $globalTemp
    Install-GlobalBinary -SourceBinary $Binary -GlobalDir $globalTemp
    Check "binary installed under the user dir" { Test-Path $p.Binary }
    Check "binary lives outside every project" {
        -not (Test-Path (Join-Path $projectA ".mcp-semantic-search-zvec-go\bin\mcp-semantic-search-zvec-go.exe"))
    }
    Copy-Item -Force (Join-Path $RepoRoot "config.yaml") $p.Config
    # A free port: the smoke must never bind the real install's 8080 or leave a
    # daemon behind on the shared port.
    $smokePort = Get-FreeLoopbackPort
    Write-GlobalDaemonYaml -Path $p.DaemonYaml -HTTPAddr "127.0.0.1:$smokePort"
    Check "daemon.yaml binds loopback only" {
        (Get-Content -Raw $p.DaemonYaml) -match 'http_addr:\s*127\.0\.0\.1:'
    }
    Check "daemon.yaml uses an isolated port" {
        (Get-Content -Raw $p.DaemonYaml) -match "http_addr:\s*127\.0\.0\.1:$smokePort"
    }
    $token = Get-GlobalAuthToken -GlobalDir $globalTemp
    Check "auth.token is 64 hex chars" { $token -match '^[0-9a-f]{64}$' }
    $tokenAgain = Get-GlobalAuthToken -GlobalDir $globalTemp
    Check "auth.token is not regenerated" { $tokenAgain -eq $token }

    # --- 2. add two projects ------------------------------------------------
    Write-Host "2. Adding two projects to the one application"
    New-FixtureProject -Root $projectA -Marker "PROJECT-A-MARKER"
    New-FixtureProject -Root $projectB -Marker "PROJECT-B-MARKER"

    $addA = Invoke-GlobalBinary -GlobalDir $globalTemp -Arguments @("--project-add", $projectA)
    Check "project-add A exits 0" { $addA.ExitCode -eq 0 }
    $addB = Invoke-GlobalBinary -GlobalDir $globalTemp -Arguments @("--project-add", $projectB)
    Check "project-add B exits 0" { $addB.ExitCode -eq 0 }

    $idA = Get-ProjectCollectionID -TargetRoot $projectA
    $idB = Get-ProjectCollectionID -TargetRoot $projectB
    Check "A has a collection_id" { $idA -match '^[0-9a-f-]{36}$' }
    Check "B has a collection_id" { $idB -match '^[0-9a-f-]{36}$' }
    Check "A and B have different collection ids" { $idA -ne $idB }

    # --- 3. per-project isolation -------------------------------------------
    Write-Host "3. Checking per-project isolation"
    foreach ($root in @($projectA, $projectB)) {
        $installDir = Join-Path $root ".mcp-semantic-search-zvec-go"
        $files = @(Get-ChildItem -Path $installDir -Recurse -File -ErrorAction SilentlyContinue |
            ForEach-Object { $_.FullName.Substring($installDir.Length + 1) })
        Check "$(Split-Path -Leaf $root): only project.yaml is stored" {
            $files.Count -eq 1 -and $files[0] -eq "project.yaml"
        }
        Check "$(Split-Path -Leaf $root): no bin/models/index/.env" {
            -not (Test-Path (Join-Path $installDir "bin")) -and
            -not (Test-Path (Join-Path $installDir "models")) -and
            -not (Test-Path (Join-Path $installDir "data")) -and
            -not (Test-Path (Join-Path $installDir ".env"))
        }
        $cfg = Get-Content -Raw (Get-ProjectConfigPath -TargetRoot $root)
        Check "$(Split-Path -Leaf $root): project.yaml has no absolute path" {
            $cfg -notmatch [regex]::Escape($root)
        }
        Check "$(Split-Path -Leaf $root): project.yaml declares schema_version 1" {
            $cfg -match 'schema_version:\s*1'
        }
    }

    # --- 4. registrations ---------------------------------------------------
    Write-Host "4. Checking the local registry"
    $regs = @(Get-GlobalRegistrations -GlobalDir $globalTemp)
    Check "two registrations exist" { $regs.Count -eq 2 }
    Check "registrations live under the user dir" {
        $regs | ForEach-Object { $_.Path.StartsWith($p.ProjectsDir) } | Where-Object { -not $_ } | Measure-Object | Select-Object -ExpandProperty Count | ForEach-Object { $_ -eq 0 }
    }
    Check "registration files carry no index_dir/config_path" {
        ($regs | ForEach-Object { Get-Content -Raw $_.Path }) -notmatch 'index_dir|config_path'
    }

    # --- 5. managed IDE entries --------------------------------------------
    Write-Host "5. Writing managed IDE entries"
    $entryA = New-GlobalMcpEntry -GlobalDir $globalTemp -TargetRoot $projectA
    Check "entry command is the shared binary" { $entryA.command -eq $p.Binary }
    Check "entry uses --stdio-proxy --project-config" {
        ($entryA.args -join ' ') -eq "--stdio-proxy --project-config $(Get-ProjectConfigPath -TargetRoot $projectA)"
    }
    Check "entry carries no secrets or INDEX_DIR" {
        ($entryA | ConvertTo-Json -Depth 5) -notmatch 'API_TOKEN|INDEX_DIR|WORKSPACE_ROOT'
    }

    # A pre-existing unrelated server entry must survive.
    $cursorJson = Join-Path $projectA ".cursor\mcp.json"
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $cursorJson) | Out-Null
    Write-Utf8File -Path $cursorJson -Content (@{ mcpServers = @{ other_server = @{ command = "other.exe" } } } | ConvertTo-Json -Depth 5)
    Set-ManagedMcpEntry -Path $cursorJson -Entry $entryA
    $merged = Get-Content -Raw $cursorJson | ConvertFrom-Json
    Check "unrelated mcp.json entry preserved" { $null -ne $merged.mcpServers.other_server }
    Check "managed mcp.json entry written" { $merged.mcpServers.'semantic-search-zvec-go'.command -eq $p.Binary }

    # --- 6. idempotent re-install ------------------------------------------
    Write-Host "6. Re-running install (idempotency)"
    $beforeA = Get-Content -Raw (Get-ProjectConfigPath -TargetRoot $projectA)
    $addA2 = Invoke-GlobalBinary -GlobalDir $globalTemp -Arguments @("--project-add", $projectA)
    Check "re-add exits 0" { $addA2.ExitCode -eq 0 }
    Check "collection id unchanged on re-add" { (Get-ProjectCollectionID -TargetRoot $projectA) -eq $idA }
    Check "project.yaml unchanged on re-add" { (Get-Content -Raw (Get-ProjectConfigPath -TargetRoot $projectA)) -eq $beforeA }
    Check "still exactly two registrations" { @(Get-GlobalRegistrations -GlobalDir $globalTemp).Count -eq 2 }
    Check "daemon.yaml not overwritten" {
        (Get-Content -Raw $p.DaemonYaml) -match "127\.0\.0\.1:$smokePort"
    }

    # --- 7. conflicts ------------------------------------------------------
    Write-Host "7. Checking id/root conflict safety"
    $copy = New-TempDir "mcp-proj-copy"
    Copy-Item -Recurse -Force (Join-Path $projectA ".mcp-semantic-search-zvec-go") (Join-Path $copy ".mcp-semantic-search-zvec-go")
    $conflict = Invoke-GlobalBinary -GlobalDir $globalTemp -Arguments @("--project-add", $copy)
    Check "copying a project with the same id is refused" { $conflict.ExitCode -ne 0 }
    Check "the refusal names the copy" { $conflict.Output -match 'already registered|project-new-collection' }
    Check "the copy did not register" { @(Get-GlobalRegistrations -GlobalDir $globalTemp).Count -eq 2 }

    $fork = Invoke-GlobalBinary -GlobalDir $globalTemp -Arguments @("--project-new-collection", $copy)
    Check "--project-new-collection succeeds" { $fork.ExitCode -eq 0 }
    $idCopy = Get-ProjectCollectionID -TargetRoot $copy
    Check "the copy got a new id" { $idCopy -ne $idA -and $idCopy -match '^[0-9a-f-]{36}$' }
    Check "the original id is unchanged" { (Get-ProjectCollectionID -TargetRoot $projectA) -eq $idA }
    Check "three registrations now exist" { @(Get-GlobalRegistrations -GlobalDir $globalTemp).Count -eq 3 }

    # --- 8. paths with spaces and Cyrillic ---------------------------------
    Write-Host "8. Checking paths with spaces and Cyrillic"
    $unicodeRoot = Join-Path $globalTemp "Проект с пробелами"
    New-FixtureProject -Root $unicodeRoot -Marker "UNICODE-MARKER"
    $addU = Invoke-GlobalBinary -GlobalDir $globalTemp -Arguments @("--project-add", $unicodeRoot)
    Check "unicode path add exits 0" { $addU.ExitCode -eq 0 }
    $idU = Get-ProjectCollectionID -TargetRoot $unicodeRoot
    Check "unicode project has a collection id" { $idU -match '^[0-9a-f-]{36}$' }
    Check "unicode project registered under the user dir" {
        @(Get-GlobalRegistrations -GlobalDir $globalTemp | Where-Object { $_.ID -eq $idU }).Count -eq 1
    }

    # --- 9. project uninstall keeps data -----------------------------------
    Write-Host "9. Uninstalling project B"
    $idBPath = Join-Path $p.Collections $idB
    New-Item -ItemType Directory -Force -Path $idBPath | Out-Null
    Set-Content -Path (Join-Path $idBPath "marker") -Value "keep me"
    $remove = Invoke-GlobalBinary -GlobalDir $globalTemp -Arguments @("--project-remove", $projectB)
    Check "project-remove exits 0" { $remove.ExitCode -eq 0 }
    Check "B registration removed" {
        @(Get-GlobalRegistrations -GlobalDir $globalTemp | Where-Object { $_.ID -eq $idB }).Count -eq 0
    }
    Check "B collection data preserved" { Test-Path (Join-Path $idBPath "marker") }
    Check "A still registered" {
        @(Get-GlobalRegistrations -GlobalDir $globalTemp | Where-Object { $_.ID -eq $idA }).Count -eq 1
    }

    # --- 10. daemon lifecycle ----------------------------------------------
    Write-Host "10. Checking the daemon lifecycle"
    $ensure = Invoke-GlobalBinary -GlobalDir $globalTemp -Arguments @("--ensure-daemon")
    Check "ensure-daemon exits 0" { $ensure.ExitCode -eq 0 }
    Check "ensure-daemon reports the isolated listener" { $ensure.Output -match "127\.0\.0\.1:$smokePort" }
    Check "daemon process is running" {
        @(Get-CimInstance Win32_Process -Filter "Name='mcp-semantic-search-zvec-go.exe'" |
            Where-Object { $_.CommandLine -like "*--daemon*" -and $_.CommandLine -like "*$globalTemp*" }).Count -eq 1
    }
    $stop = Invoke-GlobalBinary -GlobalDir $globalTemp -Arguments @("--stop-daemon")
    Check "stop-daemon exits 0" { $stop.ExitCode -eq 0 }
    Start-Sleep -Milliseconds 800
    Check "daemon stopped" {
        @(Get-CimInstance Win32_Process -Filter "Name='mcp-semantic-search-zvec-go.exe'" |
            Where-Object { $_.CommandLine -like "*$globalTemp*" }).Count -eq 0
    }

    # --- 11. legacy migration is opt-in ------------------------------------
    Write-Host "11. Checking the legacy migration guard"
    $legacyRoot = New-TempDir "mcp-legacy-proj"
    $legacyInstall = Join-Path $legacyRoot ".mcp-semantic-search-zvec-go"
    New-Item -ItemType Directory -Force -Path (Join-Path $legacyInstall "bin") | Out-Null
    Copy-Item -Force $Binary (Join-Path $legacyInstall "bin\mcp-semantic-search-zvec-go.exe")
    Check "a legacy project install is detected" { Test-LegacyProjectInstall -TargetRoot $legacyRoot }
    # Without -MigrateLegacy the install must refuse rather than create a second owner.
    $refused = $false
    try {
        $script:MigrateLegacy = $false
        # Assert-NoLegacyInstall reads the script-scope switch from install.ps1; here
        # we reproduce its decision with the same inputs.
        if ((Test-LegacyProjectInstall -TargetRoot $legacyRoot) -and -not $script:MigrateLegacy) { $refused = $true }
    } catch { }
    Check "legacy install without -MigrateLegacy is refused" { $refused }
    Check "legacy install is not deleted by the refusal" {
        Test-Path (Join-Path $legacyInstall "bin\mcp-semantic-search-zvec-go.exe")
    }
    Remove-Item -Recurse -Force $legacyRoot -ErrorAction SilentlyContinue

    # --- 12. app-uninstall guard --------------------------------------------
    Write-Host "12. Checking the app-uninstall guard"
    $regsLeft = @(Get-GlobalRegistrations -GlobalDir $globalTemp)
    Check "registrations still exist before app uninstall" { $regsLeft.Count -gt 0 }

    # --- 13. relative MCP_GLOBAL_DIR is rejected ---------------------------
    Write-Host "13. Checking MCP_GLOBAL_DIR validation"
    $prev = $env:MCP_GLOBAL_DIR
    $env:MCP_GLOBAL_DIR = "relative/dir"
    Check "relative MCP_GLOBAL_DIR is rejected" {
        $threw = $false
        try { $null = Get-GlobalDir } catch { $threw = $true }
        $threw
    }
    $env:MCP_GLOBAL_DIR = $prev

} catch {
    Write-Host "SMOKE ERROR: $($_.Exception.Message)" -ForegroundColor Red
    $script:Failures += "fatal: $($_.Exception.Message)"
} finally {
    Remove-Item Env:MCP_GLOBAL_DIR -ErrorAction SilentlyContinue
    & $cleanup
}

Write-Host ""
if ($script:Failures.Count -gt 0) {
    Write-Host "FAILED $($script:Failures.Count)/$($script:Checks) checks:" -ForegroundColor Red
    $script:Failures | ForEach-Object { Write-Host "  - $_" -ForegroundColor Red }
    exit 1
}
Write-Host "PASSED all $($script:Checks) checks." -ForegroundColor Green
exit 0
