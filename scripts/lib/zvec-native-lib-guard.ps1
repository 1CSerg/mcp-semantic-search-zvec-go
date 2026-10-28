# Shared guard: detect stale zvec native libraries left in the repository root.
#
# A zvec native library in the repo root silently wins the Windows DLL search
# order (and the Linux ld.so cwd lookup) over the pinned copy in
# .deps/zvec-go/lib/<os>_<arch>, producing "exit status 0xc0000139" or
# "cannot open shared object file" errors that look like a broken build.
# Dot-source this file and call Write-StaleZvecRootNativeLibWarning.
#
# Related plan: docs/plans/completed/2026-10-19-post-upgrade-followups.md (problem 2).

$script:ZvecNativeLibNames = @(
    "zvec_c_api.dll",
    "libzvec_c_api.so",
    "libzvec_c_api.dylib"
)

function Get-ZvecPinnedLibSha256 {
    param([Parameter(Mandatory = $true)][string]$LibDir)
    $shaFile = Join-Path $LibDir ".zvec-lib-sha256"
    if (-not (Test-Path $shaFile)) { return $null }
    $sha = (Get-Content $shaFile -Raw).Trim().ToLowerInvariant()
    if ([string]::IsNullOrEmpty($sha)) { return $null }
    return $sha
}

# Returns one object per stale root-level zvec native library:
#   @{ Name; Path; Reason }
function Get-StaleZvecRootNativeLibs {
    param(
        [Parameter(Mandatory = $true)][string]$RepoRoot,
        [string]$LibDir
    )
    $pinned = $null
    if ($LibDir) { $pinned = Get-ZvecPinnedLibSha256 -LibDir $LibDir }
    $stale = @()
    foreach ($name in $script:ZvecNativeLibNames) {
        $candidate = Join-Path $RepoRoot $name
        if (-not (Test-Path $candidate)) { continue }
        $reason = "unpinned copy in the repository root shadows .deps/zvec-go/lib"
        if ($pinned) {
            $got = (Get-FileHash -Path $candidate -Algorithm SHA256).Hash.ToLowerInvariant()
            if ($got -eq $pinned) {
                $reason = "identical to the pinned copy, but still shadows the library search order"
            } else {
                $reason = "SHA256 $($got.Substring(0, 12))... differs from pinned $($pinned.Substring(0, 12))..."
            }
        }
        $stale += [pscustomobject]@{ Name = $name; Path = $candidate; Reason = $reason }
    }
    return $stale
}

# Prints a warning (never fails the build) when stale native libs sit in the root.
function Write-StaleZvecRootNativeLibWarning {
    param(
        [Parameter(Mandatory = $true)][string]$RepoRoot,
        [string]$LibDir
    )
    $stale = @(Get-StaleZvecRootNativeLibs -RepoRoot $RepoRoot -LibDir $LibDir)
    if ($stale.Count -eq 0) { return }
    Write-Host ""
    Write-Warning "Stale zvec native library in the repository root:"
    foreach ($item in $stale) {
        Write-Warning "  $($item.Path)"
        Write-Warning "    $($item.Reason)"
    }
    Write-Warning "  The root copy is picked up before .deps/zvec-go/lib/windows_amd64 and causes"
    Write-Warning "  load failures such as 'exit status 0xc0000139'. Remove it:"
    Write-Warning "    Remove-Item '$($stale[0].Path)'"
    Write-Warning "  Binaries are reproducible with scripts/fetch/fetch-zvec-libs.ps1."
    Write-Host ""
}
