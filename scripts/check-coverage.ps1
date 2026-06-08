param(
    [int]$Min = $(if ($env:COVERAGE_MIN) { [int]$env:COVERAGE_MIN } else { 88 }),
    [int]$PkgMin = $(if ($env:COVERAGE_PKG_MIN) { [int]$env:COVERAGE_PKG_MIN } else { 50 }),
    [string]$Profile = $(if ($env:COVERAGE_PROFILE) { $env:COVERAGE_PROFILE } else { "coverage.out" }),
    [string]$Packages = $(if ($env:COVERAGE_PACKAGES) { $env:COVERAGE_PACKAGES } else { "./internal/..." })
)

$ErrorActionPreference = "Stop"
$output = go test -coverprofile="$Profile" $Packages.Split(" ") 2>&1
$output | ForEach-Object { Write-Host $_ }
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$lines = go tool cover -func="$Profile"
$totalLine = $lines | Select-String "^total:"
if (-not $totalLine) {
    Write-Error "could not parse coverage total from $Profile"
}
$total = [double]($totalLine -replace ".*\s+(\d+\.?\d*)%.*", '$1')
Write-Host ("coverage: {0}% (project minimum {1}%, packages: {2})" -f $total, $Min, $Packages)

$pkgFailed = $false
$output | Select-String "coverage: \d+\.?\d*% of statements" | ForEach-Object {
    if ($_.Line -match "coverage: ([\d.]+)%") {
        $pct = [double]$matches[1]
        $pkg = ($_.Line -split "\s+")[1]
        Write-Host ("  package {0}: {1}% (module minimum {2}%)" -f $pkg, $pct, $PkgMin)
        if ($pct -lt $PkgMin) {
            Write-Error ("package {0}: {1}% is below module minimum {2}%" -f $pkg, $pct, $PkgMin)
            $pkgFailed = $true
        }
    }
}
if ($pkgFailed) { exit 1 }

if ($total -lt $Min) {
    Write-Error ("coverage {0}% is below project minimum {1}%" -f $total, $Min)
}
