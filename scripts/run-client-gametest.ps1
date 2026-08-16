<#
.SYNOPSIS
Runs the Fabric client game test without Gradle.

.DESCRIPTION
Gradle cannot start in some environments: every task fails with "Unable to
establish loopback connection" before any build logic runs. That message names
the wrong thing -- see the preflight below, which finds the usual cause and works
around it -- but where it cannot be cleared, the client game test is the only
end-to-end exercise of capture and the only thing that re-checks the capture
fingerprint, so losing it to Gradle is losing the gate.

Everything Gradle does here is mechanical, and Loom has already written the hard
part down. .gradle\loom-cache\launch.cfg holds the launch specification, the
dependency jars are in the Gradle cache, and the sources compile with javac.
This assembles those and launches the same client.

It does NOT accept Mojang's EULA. build.gradle deliberately refuses to make that
decision for an operator, and so does this: the run directory must already carry
an eula.txt from a run you authorised.

Every line printed carries elapsed seconds, and the launch streams its
milestones rather than hiding them in a log, because a client that takes four
minutes to start and one that has stopped are otherwise the same thing to watch.

.PARAMETER BuildOnly
Compile and prepare without starting a client. Useful where a client cannot run
but the sources should still be checked.

.PARAMETER SkipBuild
Launch whatever is already compiled.

.PARAMETER TimeoutSeconds
How long the client may take before the run is abandoned.

.EXAMPLE
powershell -ExecutionPolicy Bypass -File scripts\run-client-gametest.ps1
#>
[CmdletBinding()]
param(
    [switch]$BuildOnly,
    [switch]$SkipBuild,
    [int]$TimeoutSeconds = 900
)

$ErrorActionPreference = 'Stop'

$repository = Split-Path -Parent $PSScriptRoot
$fabric = Join-Path $repository 'adapters\fabric'
$runDir = Join-Path $fabric 'build\run-gametest'
$work = Join-Path $env:TEMP 'worldledger-gametest'

# The toolchain lives beside the repository rather than on PATH.
$tools = Split-Path -Parent $repository
$jdk = Join-Path $tools '.tools\jdk25\jdk-25.0.4+7'
$gradleHome = Join-Path $tools '.tools\gradle-home'

$startedAt = Get-Date
function Elapsed { [int]((Get-Date) - $startedAt).TotalSeconds }
function Step($text) { Write-Host ("==> [{0,3}s] {1}" -f (Elapsed), $text) }
function Detail($text) { Write-Host ("    {0}" -f $text) }
function Fail($text) { Write-Host "!!  $text"; exit 1 }

if (-not (Test-Path -LiteralPath "$jdk\bin\java.exe")) { Fail "no JDK at $jdk" }
$launchCfg = Join-Path $fabric '.gradle\loom-cache\launch.cfg'
if (-not (Test-Path -LiteralPath $launchCfg)) {
    Fail @"
no $launchCfg. Loom writes it during a Gradle build, so one successful
    .\gradlew build has to have happened on this machine before this can work.
"@
}

New-Item -ItemType Directory -Force -Path $work | Out-Null

# Windows PowerShell's Set-Content -Encoding utf8 writes a byte order mark, and
# every consumer here is Java: javac rejects an argument file that starts with
# one, java.util.Properties reads it into the first key, and Fabric Loader's
# JSON parser refuses the manifest. .NET writes UTF-8 without one.
function WriteLines($path, $lines) {
    [System.IO.File]::WriteAllLines($path, [string[]]$lines)
}

# A Java argument file treats a backslash as an escape, so C:\Users becomes
# C:Users. Every path handed to the JVM goes through here.
function J($path) { $path -replace '\\', '/' }

# ---- classpath ------------------------------------------------------------
Step 'Collecting the dependency jars from the Gradle cache'

function JarsUnder($root, $pattern) {
    if (-not (Test-Path -LiteralPath $root)) { return @() }
    Get-ChildItem -LiteralPath $root -Recurse -Filter $pattern -File |
        Where-Object { $_.Name -notmatch 'sources|javadoc' } |
        ForEach-Object { $_.FullName }
}

# The runtime Minecraft jars are the ones under .gradle\loom-cache that
# launch.cfg names, not the -deobf jars used to compile against.
$runtimeEntries = @()
$runtimeEntries += JarsUnder (Join-Path $fabric '.gradle\loom-cache\minecraftMaven') '*.jar'
$runtimeEntries += JarsUnder (Join-Path $gradleHome 'caches\modules-2') '*.jar'
foreach ($set in 'main', 'client', 'gametest') {
    $runtimeEntries += (Join-Path $fabric "build\classes\java\$set")
    $runtimeEntries += (Join-Path $fabric "build\resources\$set")
}
$runtimeCp = ($runtimeEntries | ForEach-Object { J $_ }) -join ';'

$compileEntries = @()
$compileEntries += JarsUnder (Join-Path $gradleHome 'caches\fabric-loom\minecraftMaven') '*deobf-26.2.jar'
$compileEntries += JarsUnder (Join-Path $gradleHome 'caches\modules-2') '*.jar'
$compileCp = ($compileEntries | ForEach-Object { J $_ }) -join ';'
Detail "$($runtimeEntries.Count) runtime entries"

# ---- build ----------------------------------------------------------------
# Into the same directories Gradle uses, because launch.cfg names them.
function Compile($set, $extra) {
    $sourceDir = Join-Path $fabric "src\$set\java"
    if (-not (Test-Path -LiteralPath $sourceDir)) { return }
    # Source paths go in relative and the working directory is the adapter, so
    # that a repository under a path with a space in it still works: a javac
    # argument file splits on whitespace and has no way to quote a source.
    Push-Location $fabric
    try {
        $sources = Get-ChildItem -LiteralPath $sourceDir -Recurse -Filter '*.java' -File |
            ForEach-Object { (Resolve-Path -LiteralPath $_.FullName -Relative) -replace '^\.\\', '' }
        if (-not $sources) { return }
        Detail ("{0,-9} compiling {1} file(s)..." -f $set, $sources.Count)

        $out = Join-Path $fabric "build\classes\java\$set"
        if (Test-Path -LiteralPath $out) { Remove-Item -LiteralPath $out -Recurse -Force }
        New-Item -ItemType Directory -Force -Path $out | Out-Null

        $argsFile = Join-Path $work "$set-args.txt"
        $lines = @('-nowarn', '-d', "`"$(J $out)`"", '-cp', "`"$extra$compileCp`"") + $sources
        WriteLines $argsFile $lines

        & "$jdk\bin\javac.exe" "@$argsFile"
        if ($LASTEXITCODE -ne 0) { Fail "$set did not compile" }
        $count = (Get-ChildItem -LiteralPath $out -Recurse -Filter '*.class' -File).Count
        Detail ("{0,-9} {1} classes" -f $set, $count)
    } finally {
        Pop-Location
    }
}

if (-not $SkipBuild) {
    Step 'Compiling the adapter'
    $mainOut = "$(J (Join-Path $fabric 'build\classes\java\main'));"
    $clientOut = "$(J (Join-Path $fabric 'build\classes\java\client'));"
    Compile 'main' ''
    Compile 'client' $mainOut
    Compile 'gametest' "$mainOut$clientOut"

    Step 'Copying resources'
    foreach ($set in 'main', 'client', 'gametest') {
        $from = Join-Path $fabric "src\$set\resources"
        if (-not (Test-Path -LiteralPath $from)) { continue }
        $to = Join-Path $fabric "build\resources\$set"
        New-Item -ItemType Directory -Force -Path $to | Out-Null
        Copy-Item -Path "$from\*" -Destination $to -Recurse -Force
    }
    # Gradle expands ${version} during processResources. Copying the sources over
    # its output puts the placeholder back, and a manifest that still carries one
    # is not a mod Fabric Loader will load, so this has to happen every time.
    $properties = Get-Content -LiteralPath (Join-Path $fabric 'gradle.properties')
    $version = ($properties | Where-Object { $_ -match '^\s*mod_version\s*=' } |
        ForEach-Object { ($_ -split '=', 2)[1].Trim() } | Select-Object -First 1)
    if (-not $version) { Fail 'no mod_version in gradle.properties' }
    foreach ($manifest in Get-ChildItem -LiteralPath (Join-Path $fabric 'build\resources') -Recurse -Filter 'fabric.mod.json' -File) {
        $text = (Get-Content -LiteralPath $manifest.FullName -Raw).Replace('${version}', $version)
        if ($text -match '\$\{') { Fail "unexpanded placeholder left in $($manifest.FullName)" }
        [System.IO.File]::WriteAllText($manifest.FullName, $text)
    }
    Detail "mod version $version"
}

# ---- prepare the run directory -------------------------------------------
# The same values build.gradle's prepareClientGametest writes. queue_capacity is
# deliberately below the shipped default so the run exercises backpressure.
Step 'Preparing the run directory'
$configDir = Join-Path $runDir 'config\worldledger'
New-Item -ItemType Directory -Force -Path $configDir | Out-Null
WriteLines (Join-Path $configDir 'capture.properties') @(
    'contributor=client-gametest',
    'server_id=worldledger-client-gametest',
    'coalesce_ticks=10',
    'queue_capacity=8',
    'max_snapshots_per_tick=1'
)

# A spool left by an earlier run would let the fingerprint pass on old output.
$spool = Join-Path $configDir 'spool'
if (Test-Path -LiteralPath $spool) { Remove-Item -LiteralPath $spool -Recurse -Force }

$eula = Join-Path $runDir 'eula.txt'
if (-not ((Test-Path -LiteralPath $eula) -and (Select-String -LiteralPath $eula -Pattern '^eula=true' -Quiet))) {
    Fail @"
the client game test starts a Minecraft dedicated server, which requires accepting
    Mojang's End User Licence Agreement: https://aka.ms/MinecraftEULA

    This script will not accept it for you. Run the Gradle task once with
      .\gradlew runClientGametest -Pworldledger.acceptMinecraftEula=true
    or write eula=true into $eula yourself.
"@
}

if ($BuildOnly) {
    Step 'Built and prepared; not launching a client because -BuildOnly was given'
    exit 0
}

# The game test starts a Minecraft dedicated server, whose networking is Netty,
# whose event loop is a java.nio Selector. On Windows a Selector's wakeup pipe is
# a pair of AF_UNIX sockets (WEPollSelectorImpl -> PipeImpl with preferAfUnix),
# and the socket file is created in %TEMP%. Where an AF_UNIX connect() to that
# directory is refused, the JDK wraps the failure as "Unable to establish loopback
# connection" -- naming a mechanism it did not use, which is why TCP loopback
# working proves nothing, and why Gradle's identical message has this same cause.
# Minecraft reaches it minutes into startup and calls it "failed to create a child
# event loop". Two unrecognisable messages, one directory.
$preflight = Join-Path $work 'Preflight.java'
WriteLines $preflight @(
    'import java.nio.channels.Selector;',
    'public class Preflight {',
    '    public static void main(String[] args) throws Exception { Selector.open().close(); }',
    '}'
)

# Through Start-Process with the streams sent to files. Calling java directly
# would let Windows PowerShell wrap its stderr in a NativeCommandError, which
# ErrorActionPreference='Stop' turns into a terminating error, and the
# explanation below would never be reached.
$preflightLog = Join-Path $work 'preflight.log'
function CanOpenSelector($socketDir) {
    $argLine = if ($socketDir) {
        "`"-Djdk.net.unixdomain.tmpdir=$(J $socketDir)`" `"$preflight`""
    } else {
        "`"$preflight`""
    }
    $probe = Start-Process -FilePath "$jdk\bin\java.exe" -ArgumentList $argLine `
        -NoNewWindow -Wait -PassThru `
        -RedirectStandardOutput $preflightLog -RedirectStandardError "$preflightLog.err"
    return $probe.ExitCode -eq 0
}

# $null means "wherever the JDK would put it anyway", which is what a healthy
# machine uses and what leaves the launch below unchanged.
$socketDir = $null
if (-not (CanOpenSelector $null)) {
    Step 'Selector.open() fails where the JDK puts its sockets; looking for a directory that works'
    # Shortest first: an AF_UNIX path has 108 bytes to fit into, so a deep
    # checkout is a real way for the second candidate to fail.
    foreach ($candidate in @((Join-Path $env:SystemRoot 'Temp'), (Join-Path $fabric 'build\uds'))) {
        New-Item -ItemType Directory -Force -Path $candidate -ErrorAction SilentlyContinue | Out-Null
        if (-not (Test-Path -LiteralPath $candidate)) { continue }
        if (CanOpenSelector $candidate) { $socketDir = $candidate; break }
        Detail "no: $candidate"
    }
    if (-not $socketDir) {
        Fail @"
this environment cannot open a java.nio Selector from any directory tried, so the
    Minecraft dedicated server the game test needs cannot start. The same failure is
    why Gradle reports "Unable to establish loopback connection". Find a directory
    where this exits 0 and the rest follows:
      & "$jdk\bin\java.exe" "-Djdk.net.unixdomain.tmpdir=<dir>" "$preflight"
"@
    }
    Detail "sockets from $socketDir"
    Detail 'To fix it for every JVM rather than only this run -- Gradle and its daemon'
    Detail 'included -- set jdk.net.unixdomain.tmpdir to that in the JDK conf:'
    Detail "  $jdk\conf\net.properties"
}

# ---- launch ---------------------------------------------------------------
$dli = JarsUnder (Join-Path $gradleHome 'caches\modules-2') 'dev-launch-injector*.jar' | Select-Object -First 1
if (-not $dli) { Fail 'dev-launch-injector is not in the Gradle cache' }

$launchArgs = Join-Path $work 'launch-args.txt'
# Whatever the preflight had to do to open a Selector, the client has to do too.
# Built by appending rather than by an if-expression: PowerShell unwraps a
# one-element array returned from one, and a string here would concatenate itself
# onto the first argument instead of becoming a line of its own.
$socketOption = @()
if ($socketDir) { $socketOption += "`"-Djdk.net.unixdomain.tmpdir=$(J $socketDir)`"" }
WriteLines $launchArgs ($socketOption + @(
    '-cp',
    "`"$(J $dli);$runtimeCp`"",
    "`"-Dfabric.dli.config=$(J $launchCfg)`"",
    '-Dfabric.dli.env=client',
    '-Dfabric.dli.main=net.fabricmc.loader.impl.launch.knot.KnotClient',
    '-Dfabric.client.gametest=true',
    '-Dfabric.client.gametest.modid=worldledger-gametest',
    'net.fabricmc.devlaunchinjector.Main'
))

$log = Join-Path $work 'gametest.log'
$errLog = Join-Path $work 'gametest.err.log'
Step "Running the client game test (opens a Minecraft window; up to ${TimeoutSeconds}s)"
Detail "Full log: $log"
Detail 'Milestones below. A gap of a minute or two is normal while the client loads.'

$process = Start-Process -FilePath "$jdk\bin\java.exe" -ArgumentList "`"@$launchArgs`"" `
    -WorkingDirectory $runDir -RedirectStandardOutput $log -RedirectStandardError $errLog `
    -PassThru -NoNewWindow

# Follow the log while it runs. Sending nothing to the terminal for the length
# of a Minecraft start is what makes a working run and a hung one look the same.
$milestones = 'Loading Minecraft|Loading \d+ mods|Setting user|Starting integrated|Preparing spawn|Time elapsed|Stopping|capture cost|Capture session|[Gg]ame ?[Tt]est|Exception|ERROR'
$shown = 0
while (-not $process.HasExited) {
    Start-Sleep -Seconds 3
    if ((Elapsed) -gt $TimeoutSeconds) {
        $process | Stop-Process -Force -ErrorAction SilentlyContinue
        Fail "the game test did not finish within ${TimeoutSeconds}s; log: $log"
    }
    if (-not (Test-Path -LiteralPath $log)) { continue }
    $lines = @(Get-Content -LiteralPath $log -ErrorAction SilentlyContinue)
    if ($lines.Count -le $shown) { continue }
    foreach ($line in $lines[$shown..($lines.Count - 1)]) {
        if ($line -match $milestones) {
            $trimmed = if ($line.Length -gt 140) { $line.Substring(0, 140) } else { $line }
            Write-Host ("    [{0,3}s] {1}" -f (Elapsed), $trimmed)
        }
    }
    $shown = $lines.Count
}
$status = $process.ExitCode

# ---- report ---------------------------------------------------------------
$cost = Select-String -LiteralPath $log -Pattern 'client-thread capture cost' -ErrorAction SilentlyContinue |
    Select-Object -Last 1
if ($cost) { Detail $cost.Line.Trim() }

$ready = 0
if (Test-Path -LiteralPath $spool) {
    $ready = @(Get-ChildItem -LiteralPath $spool -Filter 'ready-*' -Directory -ErrorAction SilentlyContinue).Count
}
Detail "$ready ready bundle(s) in the spool"

if ($status -ne 0) {
    Write-Host ''
    Select-String -LiteralPath $log, $errLog -Pattern 'Exception|ERROR|failed' -ErrorAction SilentlyContinue |
        Select-Object -First 10 | ForEach-Object { Write-Host "    $($_.Line.Trim())" }
    Fail "the game test exited $status; full log: $log"
}
if ($ready -eq 0) {
    Fail "the run produced no bundles, so it proved nothing; log: $log"
}

Step "Passed. Log: $log"
Write-Host ''
Write-Host 'Now check the capture fingerprint has not moved:'
Write-Host '  powershell -ExecutionPolicy Bypass -File scripts\windows-capture-fingerprint.ps1 -SkipGameTest'
