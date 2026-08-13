# Controlled Minecraft 26.2 fixture

> **Automated alternative.** `./gradlew runClientGametest` in `adapters/fabric` drives a real client through a dedicated-server session and checks the bundles the adapter wrote, without anyone sitting at a keyboard. It covers the capture path end to end, fails when nothing was captured, and builds its world from a fixed seed so that two runs are comparable. The manual procedure below covers what the automated run does not assert: block entities, a sign edit, an unopened chest, and the full ordered mutation sequence.

This procedure validates the runtime event boundary that unit tests cannot exercise: a stock 26.2 multiplayer client receives a full chunk, applies controlled mutations, publishes ready bundles, and imports them through the Go core.

The procedure intentionally uses only ordinary operator commands and one normally visible chunk. It does not open the test chest.

## Required versions

Use the exact adapter baseline:

```text
Minecraft server  26.2, unmodified
Minecraft client  26.2
Java               25
Fabric Loader      0.19.3
Fabric API         0.156.0+26.2
WorldLedger mod    0.1.0-dev build under test
```

Record file hashes and environment details in [`validation-record-template.md`](validation-record-template.md) before starting.

## 1. Prepare a disposable server

Create a new 26.2 server directory and accept Mojang's EULA manually. Use a fresh world; do not run these destructive fixture commands in a valued world.

Recommended `server.properties` inputs:

```properties
level-name=worldledger-fixture-26.2
level-seed=worldledger-fixture-26.2-v1
gamemode=creative
spawn-protection=0
```

Start the server, grant the test player operator permission, join, and run the following commands in order. The three `fill` commands each stay at or below 32,768 blocks and make chunk `(0,0)` independent of terrain generation.

```mcfunction
/forceload add 0 0
/fill 0 -64 0 15 63 15 minecraft:air
/fill 0 64 0 15 191 15 minecraft:air
/fill 0 192 0 15 319 15 minecraft:air
/fill 0 63 0 15 63 15 minecraft:stone
/setblock 1 64 1 minecraft:oak_stairs[facing=north,half=bottom,shape=straight,waterlogged=false]
/setblock 2 64 1 minecraft:oak_log[axis=x]
/setblock 3 64 1 minecraft:repeater[delay=4,facing=east,locked=false,powered=false]
/setblock 4 64 1 minecraft:oak_sign[rotation=0,waterlogged=false]
/setblock 5 64 1 minecraft:chest[facing=north,type=single,waterlogged=false]
/item replace block 5 64 1 container.0 with minecraft:diamond 3
/fillbiome 0 -64 0 7 191 15 minecraft:plains
/fillbiome 0 192 0 7 319 15 minecraft:plains
/fillbiome 8 -64 0 15 191 15 minecraft:desert
/fillbiome 8 192 0 15 319 15 minecraft:desert
```

Edit the sign normally and set its first line to `WL26.2-A`. Never open the chest. Disconnect after setup so the validation run begins with a full fresh chunk packet.

If any command is rejected, stop and record the exact error rather than substituting different state. The fixture definition must be updated and versioned before comparison data is accepted.

## 2. Prepare the client spool

Build the adapter from a clean checkout and record the JAR SHA-256. Install only the baseline Fabric API and the WorldLedger JAR.

Set a dedicated contributor in `<minecraft-config>/worldledger/capture.properties`:

```properties
contributor=fixture-windows-01
server_id=worldledger-fixture-26.2
coalesce_ticks=10
queue_capacity=32
max_snapshots_per_tick=1
```

Use a distinct contributor label on Linux. Move any existing `ready-*`, `.tmp-*`, or `quarantine-*` entries to an evidence directory outside the active spool. Preserve them; do not delete unexplained entries.

Restart the client after changing configuration.

## 3. Capture the baseline and transitions

Join the disposable server and teleport to the fixture:

```mcfunction
/tp @s 8 66 8
```

Wait at least six seconds without moving out of the chunk. This allows the initial full baseline to pass both the quiet window and maximum-latency bound.

Run each mutation separately, waiting at least six seconds after each command:

```mcfunction
/setblock 2 64 1 minecraft:oak_log[axis=z]
/setblock 3 64 1 minecraft:repeater[delay=1,facing=south,locked=false,powered=false]
/fillbiome 8 -64 0 15 191 15 minecraft:forest
/fillbiome 8 192 0 15 319 15 minecraft:forest
```

Then edit the sign normally and change its first line to `WL26.2-B`. Wait another six seconds and disconnect cleanly. Do not open the chest at any point.

The exact observation count can include a conservative extra baseline or final flush. The release invariant is the ordered semantic state transition, not a hard-coded number of bundles.

## 4. Preserve and import evidence

Copy the complete spool and client log to a timestamped evidence directory before importing. Preserve `.tmp-*` and `quarantine-*` entries as failures rather than silently discarding them.

Initialize a new archive and import every `ready-*` directory in lexicographic sequence order. Example PowerShell:

```powershell
$bundles = @(Get-ChildItem .\spool -Directory -Filter 'ready-*' | Sort-Object Name)
if ($bundles.Count -eq 0) { throw 'No ready bundles were captured' }
$bundles.FullName | Set-Content -Encoding utf8 .\ready-bundles.txt

& worldledger.exe init .\archive
if ($LASTEXITCODE -ne 0) { throw 'worldledger init failed' }
foreach ($bundle in $bundles) {
  & worldledger.exe ingest-bundle --archive .\archive $bundle.FullName
  if ($LASTEXITCODE -ne 0) { throw "Import failed: $($bundle.FullName)" }
}
& worldledger.exe ingest-bundle --archive .\archive $bundles[0].FullName
if ($LASTEXITCODE -ne 0) { throw 'Duplicate-import check failed' }
& worldledger.exe fsck --archive .\archive
if ($LASTEXITCODE -ne 0) { throw 'Archive fsck failed' }
& worldledger.exe inspect --archive .\archive --server worldledger-fixture-26.2 --dimension minecraft:overworld --x 0 --z 0
if ($LASTEXITCODE -ne 0) { throw 'Archive inspection failed' }
```

POSIX shell:

```sh
set -eu
worldledger init ./archive
find ./spool -maxdepth 1 -type d -name 'ready-*' -print | sort > ./ready-bundles.txt
test -s ./ready-bundles.txt
while IFS= read -r bundle; do
  worldledger ingest-bundle --archive ./archive "$bundle"
done < ./ready-bundles.txt
first_bundle=$(sed -n '1p' ./ready-bundles.txt)
worldledger ingest-bundle --archive ./archive "$first_bundle"
worldledger fsck --archive ./archive
worldledger inspect --archive ./archive --server worldledger-fixture-26.2 --dimension minecraft:overworld --x 0 --z 0
```

Do not use `--delete-on-success` for validation evidence.

## 5. Acceptance checks

The run passes only when all of the following are recorded:

- no single-player observation exists;
- every ready bundle imports successfully and a second import is idempotent;
- `fsck` reports zero errors;
- the shape covers section Y `-4` through `19`;
- all block and biome sections are present unless a specific omission diagnostic was recorded;
- the initial state contains `oak_log[axis=x]`, the later state contains `oak_log[axis=z]`;
- the biome transition changes the affected biome component digests;
- the sign update changes `mcjava.block_entities` deterministically;
- the block-entity object contains the `minecraft:sign` network representation but contains neither `minecraft:chest` nor an `Items` key;
- client diagnostics report zero unexplained dropped final coverage, spool failures, or quarantined bundles.

CAS object paths are derived from a component digest:

```text
<archive>/objects/sha256/<digest[0:2]>/<digest[2:4]>/<digest>
```

Because the component stores block-entity type resource locations and canonical NBT as plain UTF-8, a binary-safe string scan of the `mcjava.block_entities` object can confirm that `minecraft:sign` is present while `minecraft:chest` and `Items` are absent. (`minecraft:oak_sign` is the block id, not the shared sign block-entity type.) Preserve the exact object and command output with the record.

## 6. Cross-platform comparison

Repeat the same server state and mutation sequence with a clean Windows client and a clean Linux client. `observed_at`, `received_at`, contributor, session UUID, sequence, and observation ID are expected to differ; component digests for equivalent states are not.

Comparing those by hand is no longer necessary. `worldledger fingerprint` reduces a capture to state and component digests alone, which is exactly the part that must agree:

```sh
scripts/capture-fingerprint.sh /path/to/spool this-platform.txt
worldledger fingerprint --file this-platform.txt --compare other-platform.txt
```

The automated game test is the cheaper route to the same evidence, because its world is pinned to a fixed seed, a superflat generator, and a fixed view distance. Linux CI runs it on every push and publishes its fingerprint as the `linux-capture-fingerprint` artifact, so only the Windows side has to be produced by hand.

Read the result by category. Chunks only one capture saw reflect how long each session ran and where the player went. Chunks where one capture caught a change the other missed mean the shorter session ended early; the states they share are still byte-identical. Only chunks where each side holds a state the other cannot account for indicate that the two platforms encoded the same observation differently.

That last category is a release blocker until reduced to one named component and explained. Do not update committed golden values to accommodate an unexplained live mismatch.
