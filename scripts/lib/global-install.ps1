# Shared helpers for the user-level ("global") install layout.
#
# One application per user owns every collection; each project only stores a
# project.yaml with its collection_id. This file is dot-sourced by install.ps1,
# uninstall.ps1 and the smoke scripts so the layout is defined exactly once.
#
# Layout under $GlobalDir (default %LOCALAPPDATA%\mcp-semantic-search-zvec-go):
#   bin/                      the single installed EXE + native libraries
#   config.yaml               embeddings, defaults, logging
#   daemon.yaml               loopback listener, resource caps
#   .env                      user secrets (never per project)
#   auth.token                local Bearer token for proxy <-> daemon
#   projects.d/<id>.yaml      local registry: id, root, description
#   runtime/daemon.lock       singleton daemon lock
#   data/collections/<id>/    index per collection
#   data/models/              shared model files
#   data/logs/                daemon/proxy/crash logs

# Note: no Set-StrictMode here. This file is dot-sourced into install.ps1,
# uninstall.ps1 and the smoke scripts, and strict mode would also apply to them,
# where reading $LASTEXITCODE before the first native command is an error.

$script:GlobalAppName = "mcp-semantic-search-zvec-go"
$script:GlobalBinaryName = "mcp-semantic-search-zvec-go.exe"
$script:GlobalServerKey = "semantic-search-zvec-go"
$script:GlobalManagedMarker = "managedBy: mcp-semantic-search-zvec-go"
$script:GlobalCursorRuleRel = ".cursor/rules/semantic-search-zvec-go.mdc"
$script:GlobalRooRuleRel = ".roo/rules/semantic-search-zvec-go.md"

function Write-Utf8File {
    # UTF-8 without BOM: a BOM breaks both the Go YAML/JSON readers and git diffs.
    param([Parameter(Mandatory = $true)][string]$Path, [string]$Content = "")
    $dir = Split-Path -Parent $Path
    if ($dir -and -not (Test-Path $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Content, $utf8NoBom)
}

function Get-GlobalDir {
    # MCP_GLOBAL_DIR must be absolute so every derived path is stable regardless
    # of the current directory (mirrors config.UserDataDir in Go).
    if ($env:MCP_GLOBAL_DIR) {
        $candidate = $env:MCP_GLOBAL_DIR
        if (-not [System.IO.Path]::IsPathRooted($candidate)) {
            throw "MCP_GLOBAL_DIR must be an absolute path, got '$candidate'"
        }
        return [System.IO.Path]::GetFullPath($candidate)
    }
    if (-not $env:LOCALAPPDATA) {
        throw "LOCALAPPDATA is not set; cannot determine the user-level install directory"
    }
    return (Join-Path $env:LOCALAPPDATA $script:GlobalAppName)
}

function Get-GlobalPaths {
    param([string]$GlobalDir = (Get-GlobalDir))
    return [ordered]@{
        Root        = $GlobalDir
        Bin         = Join-Path $GlobalDir "bin"
        Config      = Join-Path $GlobalDir "config.yaml"
        DaemonYaml  = Join-Path $GlobalDir "daemon.yaml"
        EnvFile     = Join-Path $GlobalDir ".env"
        AuthToken   = Join-Path $GlobalDir "auth.token"
        ProjectsDir = Join-Path $GlobalDir "projects.d"
        Runtime     = Join-Path $GlobalDir "runtime"
        Collections = Join-Path $GlobalDir "data\collections"
        Models      = Join-Path $GlobalDir "data\models"
        Logs        = Join-Path $GlobalDir "data\logs"
        Binary      = Join-Path $GlobalDir ("bin\" + $script:GlobalBinaryName)
        VersionFile = Join-Path $GlobalDir "bin\installed-version.txt"
    }
}

function Initialize-GlobalLayout {
    param([string]$GlobalDir = (Get-GlobalDir))
    $p = Get-GlobalPaths -GlobalDir $GlobalDir
    foreach ($dir in @($p.Root, $p.Bin, $p.ProjectsDir, $p.Runtime, $p.Collections, $p.Models, $p.Logs)) {
        if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    }
    return $p
}

function Get-ProjectConfigPath {
    param([string]$TargetRoot)
    return (Join-Path $TargetRoot ".mcp-semantic-search-zvec-go\project.yaml")
}

function Get-ProjectCollectionID {
    param([string]$TargetRoot)
    $path = Get-ProjectConfigPath -TargetRoot $TargetRoot
    if (-not (Test-Path $path)) { return $null }
    $m = [regex]::Match((Get-Content -Raw $path), '(?m)^\s*collection_id:\s*"?([A-Za-z0-9][A-Za-z0-9._-]*)"?\s*$')
    if (-not $m.Success) { return $null }
    return $m.Groups[1].Value
}

function Test-LegacyProjectInstall {
    # A legacy install keeps an EXE inside the project; the new layout never does.
    param([string]$TargetRoot)
    $legacyBin = Join-Path $TargetRoot ".mcp-semantic-search-zvec-go\bin"
    if (-not (Test-Path $legacyBin)) { return $false }
    return (Test-Path (Join-Path $legacyBin $script:GlobalBinaryName))
}

function Get-ProcessesUsingPath {
    # Returns the PIDs of processes whose command line references $Path. Used to
    # refuse a binary replacement while a proxy or daemon still holds the file
    # (Windows cannot replace a mapped EXE), without ever force-killing them.
    param([string]$Path)
    $found = @()
    try {
        $needle = [System.IO.Path]::GetFullPath($Path)
        foreach ($proc in Get-CimInstance Win32_Process -ErrorAction Stop) {
            if ($proc.ProcessId -eq $PID) { continue }
            $cmd = $proc.CommandLine
            if ($cmd -and $cmd.IndexOf($needle, [System.StringComparison]::OrdinalIgnoreCase) -ge 0) {
                $found += [pscustomobject]@{ PID = $proc.ProcessId; Name = $proc.Name }
            }
        }
    } catch {
        # CIM unavailable: fall back to "no known holder" and let the copy fail
        # with its own error message rather than blocking install.
        return @()
    }
    return $found
}

function Test-GlobalBinaryRuns {
    <#
    Runs `<binary> --version` and returns the reported version. Native builds need
    their libraries beside the EXE, so this must run only after the libraries are
    in place (never on a staged copy in a bare temp directory).
    #>
    param([Parameter(Mandatory = $true)][string]$Path)
    if (-not (Test-Path $Path)) { return $null }
    $reported = & $Path --version 2>&1
    if ($LASTEXITCODE -ne 0) { return $null }
    $text = ($reported | Out-String).Trim()
    if ($text -notmatch '^\d+\.\d+') { return $null }
    return $text
}

function Install-GlobalBinary {
    <#
    Installs or replaces the single application binary with staging, backup and
    rollback:
      1. verify the source binary reports a version
      2. stop the daemon through the normal admin path (never a force kill)
      3. refuse if a proxy still holds the binary
      4. move the current EXE to a backup, move the new one into place
    The post-install version check belongs to the caller, after native libraries
    are copied next to the EXE. The backup is kept until the caller calls
    Complete-GlobalBinaryInstall, so a later failure can still roll back.
    Returns the backup path ("" when there was no previous binary).
    #>
    param(
        [Parameter(Mandatory = $true)][string]$SourceBinary,
        [Parameter(Mandatory = $true)][string]$GlobalDir,
        [switch]$SkipVersionCheck
    )
    $p = Get-GlobalPaths -GlobalDir $GlobalDir
    if (-not (Test-Path $SourceBinary)) {
        throw "source binary not found: $SourceBinary"
    }
    if (-not (Test-Path $p.Bin)) { New-Item -ItemType Directory -Force -Path $p.Bin | Out-Null }

    if (-not $SkipVersionCheck) {
        $reported = Test-GlobalBinaryRuns -Path $SourceBinary
        if (-not $reported) {
            throw "source binary $SourceBinary does not report a version (run it manually to see why)"
        }
    }

    $staging = "$($p.Binary).staging"
    Copy-Item -Force $SourceBinary $staging

    $holders = Get-ProcessesUsingPath -Path $p.Binary
    if ($holders.Count -gt 0 -and (Test-Path $p.Binary)) {
        # Ask the daemon to stop gracefully first, then re-check. Pin
        # MCP_GLOBAL_DIR so the child targets this install, not an inherited one.
        $null = Invoke-GlobalBinary -GlobalDir $GlobalDir -Arguments @("--stop-daemon")
        Start-Sleep -Milliseconds 800
        $holders = Get-ProcessesUsingPath -Path $p.Binary
    }
    if ($holders.Count -gt 0) {
        Remove-Item -Force $staging -ErrorAction SilentlyContinue
        $pids = ($holders | ForEach-Object { "$($_.Name)($($_.PID))" }) -join ", "
        throw ("cannot replace $($p.Binary): still in use by $pids. " +
            "Close the IDE MCP sessions for this project and retry. The install was not changed.")
    }

    $backup = "$($p.Binary).previous"
    $hadPrevious = Test-Path $p.Binary
    if ($hadPrevious) {
        if (Test-Path $backup) { Remove-Item -Force $backup }
        Move-Item -Force $p.Binary $backup
    }
    try {
        Move-Item -Force $staging $p.Binary
    } catch {
        if (Test-Path $p.Binary) { Remove-Item -Force $p.Binary -ErrorAction SilentlyContinue }
        if ($hadPrevious -and (Test-Path $backup)) {
            Move-Item -Force $backup $p.Binary
            Write-Warning "install rolled back to the previous binary: $($_.Exception.Message)"
        }
        throw
    }
    $script:GlobalBinaryBackup = if ($hadPrevious) { $backup } else { "" }
    return $script:GlobalBinaryBackup
}

function Restore-GlobalBinary {
    <#
    Restores the previous binary after a failed post-install step (native library
    copy, version check, daemon start). Safe when no backup exists: it removes the
    new binary so the next install starts clean.
    #>
    param([Parameter(Mandatory = $true)][string]$GlobalDir)
    $p = Get-GlobalPaths -GlobalDir $GlobalDir
    $backup = $script:GlobalBinaryBackup
    if (Test-Path $p.Binary) { Remove-Item -Force $p.Binary -ErrorAction SilentlyContinue }
    if ($backup -and (Test-Path $backup)) {
        Move-Item -Force $backup $p.Binary
        Write-Warning "install rolled back to the previous binary"
    }
    $script:GlobalBinaryBackup = ""
}

function Complete-GlobalBinaryInstall {
    <#
    Drops the backup. Call it only after every post-install check passed: native
    libraries copied, --version verified, daemon healthy.
    #>
    param([Parameter(Mandatory = $true)][string]$GlobalDir)
    $p = Get-GlobalPaths -GlobalDir $GlobalDir
    $backup = "$($p.Binary).previous"
    if (Test-Path $backup) { Remove-Item -Force $backup -ErrorAction SilentlyContinue }
    $script:GlobalBinaryBackup = ""
}

function Write-GlobalDaemonYaml {
    # The user-level daemon listens on loopback only; never :8080 (all interfaces).
    # A caller may pass an explicit loopback address (smoke tests use a free port
    # so they never collide with the real install or leave a stray daemon).
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [int]$MaxOpenWorkspaces = 10,
        [string]$HTTPAddr = "127.0.0.1:8080"
    )
    if ($HTTPAddr -notmatch '^(127\.0\.0\.1|\[::1\]|localhost):\d+$') {
        throw "Write-GlobalDaemonYaml: HTTPAddr must be loopback, got '$HTTPAddr'"
    }
    $body = @"
# User-level daemon for mcp-semantic-search-zvec-go.
# One daemon owns every project collection. Loopback only: the token never
# leaves this machine.
http_addr: $HTTPAddr
max_open_workspaces: $MaxOpenWorkspaces
auto_index_on_open: true
"@
    Write-Utf8File -Path $Path -Content $body
}

function Get-FreeLoopbackPort {
    # Returns an unused loopback TCP port for isolated smoke runs.
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try {
        return $listener.LocalEndpoint.Port
    } finally {
        $listener.Stop()
    }
}

function Get-GlobalAuthToken {
    param([Parameter(Mandatory = $true)][string]$GlobalDir)
    $p = Get-GlobalPaths -GlobalDir $GlobalDir
    if (Test-Path $p.AuthToken) {
        $existing = (Get-Content -Raw $p.AuthToken).Trim()
        if ($existing.Length -ge 32) { return $existing }
        throw "auth.token at $($p.AuthToken) is corrupt; delete it and re-run install"
    }
    $bytes = New-Object byte[] 32
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    $token = -join ($bytes | ForEach-Object { $_.ToString("x2") })
    Write-Utf8File -Path $p.AuthToken -Content "$token`n"
    # Windows has no chmod: restrict the ACL to the current user and verify no
    # other ordinary principal can read it.
    try {
        $acl = Get-Acl $p.AuthToken
        $acl.SetAccessRuleProtection($true, $false)
        foreach ($rule in @($acl.Access)) { $acl.RemoveAccessRule($rule) | Out-Null }
        $acl.AddAccessRule((New-Object System.Security.AccessControl.FileSystemAccessRule(
            "$env:USERDOMAIN\$env:USERNAME", "FullControl", "Allow")))
        Set-Acl -Path $p.AuthToken -AclObject $acl
    } catch {
        Write-Warning "could not restrict ACL on $($p.AuthToken): $($_.Exception.Message)"
    }
    return $token
}

function Write-GlobalProjectConfig {
    # Writes the portable project.yaml. It never contains the root, an index path
    # or a secret: those are derived from the user-level install.
    param(
        [Parameter(Mandatory = $true)][string]$TargetRoot,
        [Parameter(Mandatory = $true)][string]$CollectionID
    )
    $path = Get-ProjectConfigPath -TargetRoot $TargetRoot
    $dir = Split-Path -Parent $path
    if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    $body = @"
schema_version: 1
collection_id: $CollectionID
"@
    Write-Utf8File -Path $path -Content $body
    return $path
}

function New-GlobalMcpEntry {
    <#
    Builds the MCP entry for a project: the single installed EXE plus
    --stdio-proxy --project-config <absolute project.yaml>. No secrets, no
    INDEX_DIR/WORKSPACE_ROOT: the proxy discovers the daemon itself.
    #>
    param(
        [Parameter(Mandatory = $true)][string]$GlobalDir,
        [Parameter(Mandatory = $true)][string]$TargetRoot
    )
    $p = Get-GlobalPaths -GlobalDir $GlobalDir
    $projectConfig = Get-ProjectConfigPath -TargetRoot $TargetRoot
    return [ordered]@{
        command = $p.Binary
        args    = @("--stdio-proxy", "--project-config", $projectConfig)
    }
}

function Set-ManagedMcpEntry {
    <#
    Writes/updates only the managed server key in a Cursor/Roo mcp.json, leaving
    every other server entry untouched.
    #>
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)]$Entry,
        [string]$ServerKey = $script:GlobalServerKey
    )
    $obj = [ordered]@{}
    $servers = @{}
    if (Test-Path $Path) {
        try {
            $existing = Get-Content -Raw $Path | ConvertFrom-Json
            foreach ($prop in $existing.PSObject.Properties) {
                if ($prop.Name -eq "mcpServers") {
                    if ($null -ne $prop.Value) {
                        foreach ($sp in $prop.Value.PSObject.Properties) { $servers[$sp.Name] = $sp.Value }
                    }
                } else {
                    $obj[$prop.Name] = $prop.Value
                }
            }
        } catch {
            throw "cannot parse $Path as JSON; fix or remove it before installing ($($_.Exception.Message))"
        }
    }
    $servers[$ServerKey] = $Entry
    $obj["mcpServers"] = $servers
    Write-Utf8File -Path $Path -Content ($obj | ConvertTo-Json -Depth 10)
}

function Remove-ManagedMcpEntry {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [string]$ServerKey = $script:GlobalServerKey
    )
    if (-not (Test-Path $Path)) { return $false }
    $obj = [ordered]@{}
    $servers = @{}
    try {
        $existing = Get-Content -Raw $Path | ConvertFrom-Json
    } catch {
        return $false
    }
    foreach ($prop in $existing.PSObject.Properties) {
        if ($prop.Name -eq "mcpServers") {
            if ($null -ne $prop.Value) {
                foreach ($sp in $prop.Value.PSObject.Properties) {
                    if ($sp.Name -ne $ServerKey) { $servers[$sp.Name] = $sp.Value }
                }
            }
        } else {
            $obj[$prop.Name] = $prop.Value
        }
    }
    $obj["mcpServers"] = $servers
    Write-Utf8File -Path $Path -Content ($obj | ConvertTo-Json -Depth 10)
    return $true
}

function Test-ManagedRuleFile {
    # Install-managed rules carry a marker so uninstall never deletes a
    # hand-written rule.
    param([string]$Path)
    if (-not (Test-Path $Path)) { return $false }
    $head = Get-Content -Raw -TotalCount 12 $Path
    return ($head -match [regex]::Escape($script:GlobalManagedMarker))
}

function Get-GlobalRegistrations {
    param([Parameter(Mandatory = $true)][string]$GlobalDir)
    $p = Get-GlobalPaths -GlobalDir $GlobalDir
    if (-not (Test-Path $p.ProjectsDir)) { return @() }
    $out = @()
    foreach ($file in Get-ChildItem -Path $p.ProjectsDir -Filter "*.yaml" -File) {
        $raw = Get-Content -Raw $file.FullName
        $id = ([regex]::Match($raw, '(?m)^id:\s*(\S+)')).Groups[1].Value
        $root = ([regex]::Match($raw, '(?m)^root:\s*(.+)$')).Groups[1].Value.Trim()
        $desc = ([regex]::Match($raw, '(?m)^description:\s*(.+)$')).Groups[1].Value.Trim()
        $out += [pscustomobject]@{ ID = $id; Root = $root; Description = $desc; Path = $file.FullName }
    }
    return $out
}

function Invoke-GlobalBinary {
    # Runs the installed binary and returns its exit code plus combined output.
    param(
        [Parameter(Mandatory = $true)][string]$GlobalDir,
        [Parameter(Mandatory = $true)][string[]]$Arguments
    )
    $p = Get-GlobalPaths -GlobalDir $GlobalDir
    if (-not (Test-Path $p.Binary)) {
        throw "installed binary not found: $($p.Binary)"
    }
    $previous = $env:MCP_GLOBAL_DIR
    $env:MCP_GLOBAL_DIR = $GlobalDir
    try {
        $output = & $p.Binary @Arguments 2>&1
        return [pscustomobject]@{ ExitCode = $LASTEXITCODE; Output = ($output -join "`n") }
    } finally {
        if ($null -eq $previous) { Remove-Item Env:MCP_GLOBAL_DIR -ErrorAction SilentlyContinue }
        else { $env:MCP_GLOBAL_DIR = $previous }
    }
}
