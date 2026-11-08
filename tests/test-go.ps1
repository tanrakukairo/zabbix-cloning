$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$module = Join-Path $root 'go-lang'
$testRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot 'go')).Path
$overlay = [IO.Path]::GetTempFileName()
try {
    # Add the mirrored files to their packages only for go test.
    $replace = @{}
    foreach ($test in (Get-ChildItem -LiteralPath $testRoot -Recurse -File -Filter '*_test.go')) {
        $relative = $test.FullName.Substring($testRoot.Length + 1)
        $replace[(Join-Path $module $relative)] = $test.FullName
    }
    if ($replace.Count -eq 0) { throw 'No Go test files found' }
    [IO.File]::WriteAllText($overlay, (@{Replace=$replace} | ConvertTo-Json -Compress))

    Push-Location -LiteralPath $module
    try {
        go test "-overlay=$overlay" @args ./...
        $status = $LASTEXITCODE
    } finally {
        Pop-Location
    }
} finally {
    Remove-Item -LiteralPath $overlay -Force
}
exit $status
