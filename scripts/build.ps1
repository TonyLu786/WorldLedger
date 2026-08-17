<#
.SYNOPSIS
Builds the command-line tools into bin\.

.DESCRIPTION
The documented commands are written as `go run ./cmd/...`, which assumes Go is
on PATH. Where it is not -- including the setup this repository is developed in,
where the toolchain sits beside the checkout under .tools rather than being
installed -- every documented command fails at the first word, and the failure
says nothing about what to do instead.

This finds Go wherever it is and builds the two tools an operator runs, so the
rest of the documentation can be followed by name.

The other three commands under cmd\ are not built. dfurenames needs javap and a
Mojang-mapped jar, and mcjava-fixtures and visualfixture exist to maintain this
repository's own fixtures; none of them is something an operator runs against
their own archive.

.PARAMETER Test
Run the test suite as well, using the same toolchain.

.EXAMPLE
powershell -ExecutionPolicy Bypass -File scripts\build.ps1
#>
[CmdletBinding()]
param(
    [switch]$Test
)

$ErrorActionPreference = 'Stop'

$repository = Split-Path -Parent $PSScriptRoot
$binDir = Join-Path $repository 'bin'

function Step($text) { Write-Host "==> $text" }
function Detail($text) { Write-Host "    $text" }
function Fail($text) { Write-Host "!!  $text"; exit 1 }

# PATH first, so a machine with Go installed uses its own. The pinned toolchain
# beside the checkout is the fallback, which is where this repository keeps it.
$go = $null
$onPath = Get-Command go -ErrorAction SilentlyContinue
if ($onPath) {
    $go = $onPath.Source
} else {
    $tools = Split-Path -Parent $repository
    $candidates = @(Get-ChildItem -Path (Join-Path $tools '.tools') -Filter 'go*' -Directory -ErrorAction SilentlyContinue |
        ForEach-Object { Join-Path $_.FullName 'go\bin\go.exe' } |
        Where-Object { Test-Path -LiteralPath $_ } |
        Sort-Object -Descending)
    if ($candidates.Count -gt 0) { $go = $candidates[0] }
}
if (-not $go) {
    Fail @"
no Go toolchain. Install Go 1.23 or newer and put it on PATH, or place one
    under .tools beside this checkout as .tools\go<version>\go\bin\go.exe
"@
}

Step "Go: $go"
Detail (& $go version)

if (-not (Test-Path -LiteralPath $binDir)) {
    New-Item -ItemType Directory -Path $binDir | Out-Null
}

if ($Test) {
    Step 'go test ./...'
    & $go test ./...
    if ($LASTEXITCODE -ne 0) { Fail 'tests failed' }
}

foreach ($command in @('worldledger', 'mcprofile')) {
    Step "building $command"
    $output = Join-Path $binDir "$command.exe"
    & $go build -trimpath -o $output "./cmd/$command"
    if ($LASTEXITCODE -ne 0) { Fail "building $command failed" }
    Detail $output
}

Write-Host ''
Write-Host 'Built. Run them from the repository root:'
Write-Host '  .\bin\worldledger.exe status --archive .\archive'
Write-Host '  .\bin\mcprofile.exe --from profiles\minecraft-java-1.21.11.json --to profiles\minecraft-java-26.2.json'
