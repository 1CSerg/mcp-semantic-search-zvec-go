param(
    [int]$Min = $(if ($env:COVERAGE_MIN) { [int]$env:COVERAGE_MIN } else { 80 }),
    [string]$Profile = $(if ($env:COVERAGE_PROFILE) { $env:COVERAGE_PROFILE } else { "coverage.out" }),
    [string]$Packages = $(if ($env:COVERAGE_PACKAGES) { $env:COVERAGE_PACKAGES } else { "./internal/..." })
)

$ErrorActionPreference = "Stop"
go test -coverprofile="$Profile" $Packages.Split(" ")
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$lines = go tool cover -func="$Profile"
$totalLine = $lines | Select-String "^total:"
if (-not $totalLine) {
    Write-Error "could not parse coverage total from $Profile"
}
$total = [double]($totalLine -replace ".*\s+(\d+\.?\d*)%.*", '$1')
Write-Host ("coverage: {0}% (minimum {1}%, packages: {2})" -f $total, $Min, $Packages)
if ($total -lt $Min) {
    Write-Error ("coverage {0}% is below minimum {1}%" -f $total, $Min)
}
