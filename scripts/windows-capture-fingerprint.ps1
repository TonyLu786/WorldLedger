<#
.SYNOPSIS
Runs the capture game test on this machine and writes the fingerprint that a
Linux capture is compared against.

.DESCRIPTION
The last open item in the capture milestone is whether the same observed world
state canonicalizes to the same bytes on two platforms. Linux CI publishes its
half on every push. This produces the other half.

Every step is one someone would otherwise run by hand, in the order that gets
them wrong the least: the game test first, then the spool imported into a
throwaway archive, then the fingerprint. The spool is left alone, because it is
the evidence.

.PARAMETER Out
Where to write the fingerprint. Defaults to the repository root.

.PARAMETER SkipGameTest
Fingerprint whatever is already in the spool without running the game test
again.

Only use this when a run has just finished with the current code. A spool left
by an older build was captured from a different world: the game test pins the
seed and generator so two platforms observe the same thing, and a spool from
before that pinning cannot be compared against anything. The comparison would
report differences that are the world's rather than the encoder's.

.EXAMPLE
powershell -ExecutionPolicy Bypass -File scripts\windows-capture-fingerprint.ps1
#>
[CmdletBinding()]
param(
    [string]$Out = '',
    [switch]$SkipGameTest
)

$ErrorActionPreference = 'Stop'

$repository = Split-Path -Parent $PSScriptRoot
$fabric = Join-Path $repository 'adapters\fabric'
$spool = Join-Path $fabric 'build\run-gametest\config\worldledger\spool'
if (-not $Out) { $Out = Join-Path $repository 'windows-capture-fingerprint.txt' }

function Step($text) { Write-Host "==> $text" }
function Fail($text) { Write-Host "!!  $text"; exit 1 }

# The toolchain lives beside the repository rather than on PATH.
$tools = Split-Path -Parent $repository
$jdk = Join-Path $tools '.tools\jdk25\jdk-25.0.4+7'
$gradleHome = Join-Path $tools '.tools\gradle-home'
$goBin = Join-Path $tools '.tools\go1.23.12\go\bin'

if (-not $SkipGameTest) {
    if (-not (Test-Path -LiteralPath $jdk)) { Fail "no JDK at $jdk" }
    $env:JAVA_HOME = $jdk
    if (Test-Path -LiteralPath $gradleHome) { $env:GRADLE_USER_HOME = $gradleHome }

    Step 'Running the capture game test. This starts a real client and takes several minutes.'
    Write-Host '    Mojang requires the EULA to be accepted for the dedicated server this starts.'
    Write-Host '    Passing the flag below is your acceptance: https://aka.ms/MinecraftEULA'
    Push-Location $fabric
    try {
        # The property name contains a dot, which PowerShell splits on unless
        # the whole argument is quoted.
        & .\gradlew.bat runClientGametest '-Pworldledger.acceptMinecraftEula=true'
        if ($LASTEXITCODE -ne 0) { Fail "the game test failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
} else {
    Step 'Skipping the game test; using the spool already on disk.'
    Write-Host '    This is only comparable if that spool came from a run of the current'
    Write-Host '    code. The game test pins the world seed and generator so two platforms'
    Write-Host '    observe the same thing; a spool from before that pinning describes a'
    Write-Host '    different world, and comparing it would report the world as a defect.'
}

if (-not (Test-Path -LiteralPath $spool)) { Fail "no spool at $spool" }
$bundles = @(Get-ChildItem -LiteralPath $spool -Directory -Filter 'ready-*')
if ($bundles.Count -eq 0) { Fail "the spool at $spool holds no ready bundle" }
Step "The spool holds $($bundles.Count) bundle(s)."

Step 'Building the command-line tool.'
if (Test-Path -LiteralPath $goBin) { $env:PATH = "$goBin;$env:PATH" }
$worldledger = Join-Path $repository 'bin\worldledger.exe'
Push-Location $repository
try {
    & go build -trimpath -o $worldledger .\cmd\worldledger
    if ($LASTEXITCODE -ne 0) { Fail 'could not build the command-line tool' }
} finally {
    Pop-Location
}

# A throwaway archive. The fingerprint describes what was captured, not what
# any particular archive happens to hold, so this one is discarded afterwards.
$archive = Join-Path ([System.IO.Path]::GetTempPath()) ("worldledger-fingerprint-" + [System.Guid]::NewGuid())
Step 'Importing the spool into a throwaway archive.'
& $worldledger init $archive | Out-Null
if ($LASTEXITCODE -ne 0) { Fail 'could not create the archive' }

# --keep, because the spool is the evidence and this script is a measurement.
& $worldledger ingest-spool --archive $archive --keep $spool
if ($LASTEXITCODE -ne 0) { Fail 'the spool did not import cleanly' }

Step 'Checking the archive before trusting what it says.'
& $worldledger fsck --archive $archive | Out-Null
if ($LASTEXITCODE -ne 0) { Fail 'the archive failed its integrity check' }

# A fingerprint only describes what the world contained. It cannot say whether
# the world contained what it was meant to: the game test places its fixture
# with server commands whose results nobody reads, so a block a release renames
# places nothing, fails nothing, and produces a smaller fingerprint that is just
# as green. This says which of the shapes are actually there.
Step 'Checking the capture still contains every shape the fixture is meant to have.'
& $worldledger corpus --archive $archive --server worldledger-client-gametest
if ($LASTEXITCODE -ne 0) { Fail 'the capture is missing shapes the fixture world is meant to contain' }

Step 'Writing the fingerprint.'
& $worldledger fingerprint --archive $archive --out $Out
if ($LASTEXITCODE -ne 0) { Fail 'could not write the fingerprint' }

Remove-Item -LiteralPath $archive -Recurse -Force -ErrorAction SilentlyContinue

# The committed reference is the gate CI enforces on every push, and until now
# running this by hand stopped one step short of it: it wrote a file and left
# the comparison to whoever remembered. A capture that moved is exactly what
# nobody notices by reading a digest.
$reference = Join-Path $repository 'testdata\capture-fingerprint-reference.txt'
if (Test-Path -LiteralPath $reference) {
    Step 'Comparing against the committed reference.'
    & $worldledger fingerprint --file $Out --compare $reference
    if ($LASTEXITCODE -ne 0) {
        Fail @"
this capture does not match testdata\capture-fingerprint-reference.txt.

    Either the encoder changed what it writes, which is a specification-level
    decision and needs one, or the game test observed a different world. Do not
    update the reference to make this pass without knowing which.
"@
    }
    Write-Host ''
    Write-Host "Fingerprint written to $Out and matches the committed reference."
} else {
    Write-Host ''
    Write-Host "Fingerprint written to $Out"
    Write-Host 'There is no committed reference to compare against yet.'
}

Write-Host ''
Write-Host 'To compare against the Linux half instead, downloaded from the'
Write-Host 'linux-capture-fingerprint artifact of any successful ci run:'
Write-Host ''
Write-Host "  bin\worldledger.exe fingerprint --file `"$Out`" --compare linux-capture-fingerprint.txt"
