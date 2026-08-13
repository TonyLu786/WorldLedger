# Fabric 26.2 capture adapter

This module is the client-only Minecraft Java producer for WorldLedger. It snapshots state already applied to the vanilla client, canonicalizes it as `worldledger.minecraft.java.chunk/v1`, and publishes crash-safe `worldledger.capture-bundle/v1` directories. It has no archive writer, upload client, or server entrypoint.

## Exact baseline

```text
Minecraft      26.2
Java           25
Fabric Loader  0.19.3
Fabric API     0.156.0+26.2
Fabric Loom    1.17.17
Gradle         9.5.1
Names          native Mojang names used by 26.2
```

The Gradle wrapper distribution is checksum-pinned. Dependency versions are fixed in [`gradle.properties`](gradle.properties); do not replace them with ranges or snapshots in a release build.

## Build

Install a Java 25 JDK, then run from this directory:

```sh
./gradlew clean build --warning-mode all
```

PowerShell:

```powershell
.\gradlew.bat clean build --warning-mode all
```

The build compiles the client mixins against the exact 26.2 classes, runs the shared Java/Go golden vectors, validates spool recovery and queue behavior, and compares a generated ready bundle byte-for-byte with the committed end-to-end fixture.

The installable output is:

```text
build/libs/worldledger-fabric-0.1.0-dev.jar
```

Install it in a client with the exact Fabric Loader and Fabric API versions above. This mod must not be installed on a dedicated server.

## Configuration

On first client start the adapter creates:

```text
<minecraft-config>/worldledger/capture.properties
```

Default contents:

```properties
contributor=
server_id=
coalesce_ticks=10
queue_capacity=8
max_snapshots_per_tick=1
```

- `contributor` is required. Blank disables capture without opening a session.
- `server_id` is an optional stable archive identifier. Blank uses the normalized server address.
- `coalesce_ticks` is the quiet period before a dirty snapshot; sustained updates still flush under a bounded maximum latency.
- `queue_capacity` bounds semantic snapshots waiting for canonicalization and disk I/O.
- `max_snapshots_per_tick` bounds normal client-thread snapshot work.

Configuration is loaded once during client startup. Restart after changing it.

## Spool contract

Ready bundles are published under:

```text
<minecraft-config>/worldledger/spool/ready-<session-uuid>-<20-digit-sequence>
```

Publication follows this order:

1. encode canonical components on the single writer thread;
2. write and force each component in a unique `.tmp-<uuid>` directory;
3. write and force `bundle.json`;
4. force the temporary directory where supported;
5. atomically move it to a unique `ready-*` name without replacing an existing target;
6. force the spool directory where supported.

A JVM lock and an operating-system file lock serialize compliant writers using the same spool. At startup, complete temporary bundles are revalidated with the same manifest, path, count, size, digest, and aggregate bounds before publication. Invalid temporary bundles are moved to `quarantine-<uuid>` and reported in the client log. Recovery hashes component files as a stream and never trusts manifest sizes.

The Go importer is intentionally separate:

```sh
worldledger ingest-bundle --archive ./archive <ready-directory>
worldledger fsck --archive ./archive
```

Do not point `--delete-on-success` at a bundle tree that overlaps the archive. The importer rejects either direction of overlap.

## Capture semantics

The runtime hooks packet handlers at `TAIL`, after vanilla has applied the update. Fabric chunk lifecycle events provide load and pre-discard unload boundaries.

```text
applied full chunk / block / biome / block-entity update
                         ↓
packet-derived block-entity knowledge cache where required
                         ↓
dirty chunk tracker: quiet-window coalescing + max latency
                         ↓
bounded client-thread semantic snapshot
                         ↓
single background canonicalization/spool writer
```

Blocks and biomes come from full semantic client chunk sections, in the axis order specified by `minecraft-java-chunk-v1`. Registry numeric IDs and runtime palette order are never persisted.

Block-entity data comes only from full-chunk update tags and individual block-entity data packets. Arbitrary `BlockEntity` serialization is not used. A known empty network-update baseline is represented by a present empty component; a missing or invalid baseline omits the component. Type changes and removals invalidate cached packet state conservatively.

On chunk unload, dimension transition, disconnect, and client stop, the adapter attempts a final dirty snapshot while the old client data remains available. It reports queue backpressure, snapshot failures, spool failures, and dropped final coverage. It never grows the queue without bound.

## Collection boundary

Version `0.1.0-dev` captures only normal multiplayer client visibility:

- block states;
- biome samples;
- block-entity network update NBT.

It does not capture:

- single-player worlds;
- chat or player messages;
- login, session, authentication, or account data;
- player or entity state;
- opened-container inventory contents;
- lighting, heightmaps, structures, or scheduled ticks;
- packets unrelated to the declared chunk components;
- unloaded or hidden chunks;
- remote uploads.

If a registry value or NBT value cannot be represented within the canonical limits, the affected component is omitted and a diagnostic is retained. It is never replaced with a fabricated default.

## Controlled 26.2 validation

The automated Java-to-Go boundary is committed under `testdata/e2e-capture-bundle`. A real client session remains a separate release gate because byte-for-byte fixture generation cannot prove event timing in a running game.

Follow [`../../examples/minecraft-26.2-fixture/README.md`](../../examples/minecraft-26.2-fixture/README.md) for the deterministic vanilla fixture, mutation sequence, spool import, and evidence to record. Until that procedure has been completed on both Windows and Linux, treat live hook behavior as implemented but not field-validated.
