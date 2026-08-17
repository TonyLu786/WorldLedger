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
    # Highest-sorting name wins. That is not a version comparison and is not
    # claimed to be one; it only has to be deterministic when more than one
    # toolchain has been unpacked there.
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

# go resolves ./cmd/... and finds go.mod relative to the working directory, so
# this has to run in the repository rather than wherever it was invoked from.
# The whole point of the script is that it can be called by path from anywhere.
Push-Location -LiteralPath $repository
try {
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
} finally {
    Pop-Location
}

# The desktop application is a separate module, so it is built where its go.mod
# is rather than by a relative package path from the root, which does not work
# across a module boundary.
Push-Location -LiteralPath (Join-Path $repository 'desktop')
try {
    if ($Test) {
        Step 'go test ./... (desktop)'
        & $go test ./...
        if ($LASTEXITCODE -ne 0) { Fail 'desktop tests failed' }
    }
    Step 'building worldledger-desktop'
    $output = Join-Path $binDir 'worldledger-desktop.exe'
    # -H=windowsgui keeps a console window from appearing behind the
    # application's own. Somebody who double-clicks this should get one window.
    & $go build -trimpath -ldflags '-H=windowsgui' -o $output .
    if ($LASTEXITCODE -ne 0) { Fail 'building worldledger-desktop failed' }
    Detail $output
} finally {
    Pop-Location
}

# Printed with full paths so the lines can be pasted anywhere, which is the
# same reason the script itself no longer depends on where it was called from.
$profiles = Join-Path $repository 'profiles'
Write-Host ''
Write-Host 'Built. These run from any directory:'
Write-Host ('  & "{0}" version' -f (Join-Path $binDir 'worldledger.exe'))
Write-Host ('  & "{0}" --from "{1}" --to "{2}"' -f
    (Join-Path $binDir 'mcprofile.exe'),
    (Join-Path $profiles 'minecraft-java-1.21.11.json'),
    (Join-Path $profiles 'minecraft-java-26.2.json'))
