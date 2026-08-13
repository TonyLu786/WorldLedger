# Milestone: Java 26.2 capture loop

The milestone is complete when a stock Minecraft Java 26.2 multiplayer session can produce a deterministic WorldLedger archive without putting archive code inside the game process.

**Current status:** Work packages A through F are implemented and pass, including a live client run. The automated client game test drove a real 26.2 client through a dedicated-server session and produced 50 capture bundles, all of which imported cleanly into a Go archive. Only the second-platform (Linux) record remains before the milestone is complete on both platforms.

This milestone covers the *capture* direction only. Reconstruction in the other direction has since been verified against a real 26.2 client; see [`status.md`](status.md). Verifying one direction says nothing about the other.

Automating the outstanding run is practical: `fabric-client-gametest-api-v1` is already in the adapter's dependency tree and is intended for driving a real client under test.

## Work package A — bundle ingress

**Status:** complete in the Go core.

Implement `worldledger.capture-bundle/v1` in the Go core.

Deliverables:

- `internal/bundle` manifest parser and validator;
- safe relative-path handling;
- independent SHA-256 and size verification;
- idempotent import into CAS and the observation ledger;
- `worldledger ingest-bundle --archive DIR <bundle-dir>`;
- optional `--delete-on-success` only after a fully successful import;
- hostile-input tests from `docs/test-strategy.md`.

Acceptance:

```text
valid bundle       -> one valid observation
same bundle twice  -> no duplicate index entry
wrong hash         -> rejected, archive remains fsck-clean
path traversal     -> rejected
symlink escape     -> rejected where testable on platform
```

Do not change observation hash semantics in this work package.

## Work package B — canonical fixture library

**Status:** complete in the Go reference implementation.

Create language-neutral golden fixtures for `worldledger.minecraft.java.chunk/v1`.

Deliverables:

- committed binary expected outputs under `testdata/mcjava-v1/`;
- machine-readable fixture descriptions;
- expected SHA-256 for every output;
- a small Go reference encoder for fixture generation/validation, kept separate from Minecraft-specific runtime code;
- tests that consume committed expected bytes rather than regenerating expected values at test time.

Acceptance:

- fixtures cover every case listed in `docs/test-strategy.md`;
- two consecutive fixture builds are byte-identical;
- fixture hashes are documented in a checked-in manifest.

Changing a fixture hash requires a specification-level explanation in the change description.

## Work package C — Fabric module bootstrap

**Status:** complete and clean-build verified.

Create the client-only adapter under `adapters/fabric`.

Baseline:

```text
Minecraft      26.2
Java           25
Fabric Loader  0.19.3
Fabric API     0.156.0+26.2
Mappings       Mojang official
Loom           stable 1.17.x, pinned exactly
```

Deliverables:

- reproducible Gradle wrapper/build;
- client-only mod metadata;
- no server-side entrypoint;
- adapter config directory and local spool path;
- unit-testable canonical encoder package;
- Java tests against the same committed golden fixtures as the Go reference implementation.

Acceptance:

```text
./gradlew build
```

passes from a clean checkout with the documented JDK.

Do not add packet capture or mixins until the canonical encoder tests are green.

## Work package D — chunk baseline capture

**Status:** implemented and compiled against the pinned 26.2 client API; live fixture acceptance remains pending.

Capture a complete terrain/biome baseline when a full client chunk becomes available.

Deliverables:

- multiplayer session lifecycle;
- dimension key and build-range discovery;
- full block-section canonicalization;
- full biome-section canonicalization;
- chunk dirty-state registry;
- initial bundle emission after the full baseline is applied.

Acceptance:

- visiting the same unchanged test chunk in two independent clean sessions produces identical canonical component digests;
- negative section Y is covered;
- runtime palette order does not affect output;
- no capture occurs for single-player unless explicitly enabled by a later design change.

## Work package E — updates and block entities

**Status:** implemented with deterministic unit tests for dirty coalescing, unknown/empty block-entity state, bounded queue ordering, ready publication, and temporary-bundle recovery. Live packet-order acceptance remains pending.

Add continuous mirroring semantics.

Deliverables:

- block-update dirty marking;
- coalesced snapshots after applied state changes;
- packet-derived block-entity update cache;
- correct invalidation/removal when block entities disappear;
- final dirty flush before unload/disconnect/dimension transition when data remains available;
- bounded work queue and observable dropped-coverage diagnostics.

Acceptance:

- the controlled integration world produces the expected state transition sequence;
- an unopened container is never represented as a known-empty inventory by v1;
- updating a sign or other synced block entity changes `mcjava.block_entities` deterministically;
- repeated updates to one chunk are coalesced rather than producing one bundle per packet.

## Work package F — end-to-end integration

**Status:** automated boundary complete; controlled live client sequence pending.

The committed fixture under `testdata/e2e-capture-bundle` is generated by the current Java spool writer, compared byte-for-byte in Java tests, imported twice by Go, checked against fixed observation/state/component digests, and verified with archive `fsck`.

Wire ready bundles through the Go importer and validate the archive.

Acceptance sequence:

```text
1. start controlled 26.2 server
2. join with Fabric adapter
3. walk through fixture area
4. perform documented block changes
5. disconnect cleanly
6. ingest every ready bundle
7. worldledger fsck --archive <dir>
8. worldledger inspect / verify expected chunks
```

The live milestone is complete when the same scripted test sequence in [`../examples/minecraft-26.2-fixture/README.md`](../examples/minecraft-26.2-fixture/README.md) produces the same canonical component digests on Windows and Linux, excluding observation timestamps and provenance-derived observation ids that are expected to differ between sessions.

## Explicitly deferred

The milestone does not include:

- entities;
- container content capture;
- Anvil export;
- public upload;
- contributor signatures;
- web UI;
- consensus scoring;
- server-specific anti-cheat bypasses;
- remote chunk scanning beyond normal client visibility.
