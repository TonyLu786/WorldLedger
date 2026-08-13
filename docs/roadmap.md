# Roadmap

The roadmap is ordered around irreversible interfaces first. Public archives become difficult to migrate once contributors and mirrors depend on them.

## Phase 0 — archive core

**Goal:** make observation identity and conflict-preserving history executable.

- [x] content-addressed object store
- [x] immutable observation model
- [x] per-chunk local index
- [x] deterministic state digest
- [x] deterministic observation id
- [x] basic corroboration/conflict analysis
- [x] CLI development workflow
- [x] archive integrity scan
- [x] portable archive manifest
- [x] multi-component capture-bundle ingest in one command
- [x] benchmark fixtures

Exit criterion: the same fixture ingested on different machines produces identical object and observation identities.

## Phase 1 — Java capture adapter

**Goal:** capture the minimum useful client-observable world state from current Java Edition multiplayer sessions.

- [x] capture-bundle v1 importer in the Go core
- [x] Java canonical component encoders with cross-language golden fixtures
- [x] Fabric 26.2 client module and reproducible build
- [x] chunk baseline/load/unload tracking
- [x] block-update dirty tracking and coalescing
- [x] biome capture
- [x] packet-derived block-entity update baseline
- [x] dimension/session transitions
- [x] bounded canonicalization queue
- [x] crash-safe spool using capture-bundle v1
- [x] controlled vanilla integration fixture
- [x] Linux client record
- [ ] cross-platform digest comparison

The integration fixture is now an automated client game test rather than only a written procedure. It passed against a real 26.2 client on Windows: 158 ready bundles, none dropped, all imported, idempotent on repeat, `fsck` clean. The same test runs headless in Linux CI on every push. What remains is comparing the two: CI does not publish the bundles it produces, so no run has yet shown that the same world state yields identical digests on both platforms.

Exit criterion: **met on Windows.** A live multiplayer session produced a deterministic local archive that then exported to a playable world. See [`status.md`](status.md).

## Phase 2 — playable reconstruction

**Goal:** turn an archive epoch into a valid single-player world.

- [x] canonical component decoding
- [x] archive enumeration
- [x] select a reconstruction time
- [x] choose observations under an explicit merge policy
- [x] convert canonical chunks to Anvil region files
- [x] validate block entities and data versions
- [x] mark unresolved/unknown state
- [x] release capability profiles
- [x] cross-release translation with declared, auditable loss
- [ ] regression worlds for Minecraft upgrades
- [ ] converted world opened in an older release

`level.dat` is deliberately not generated. It carries the data version, generator, and build height, so an export is written into a world the target client created rather than into one this project invented. This is a decision, not a missing feature.

Exit criterion: **met for the faithful export path.** An exported chunk loads and renders correctly in an unmodified Minecraft 26.2 client, including negative sections and block state properties, verified in game and by an independent reader. See [`status.md`](status.md). The conversion path has not had an equivalent run.

## Phase 2.5 — publication controls

**Goal:** make the decision to distribute an archive explicit before a public archive exists.

- [x] per-server publication policy with attribution
- [x] embargo with an expiry
- [x] coverage exposure assessment over the merged archive
- [x] operations that build a shareable world require a declared policy
- [ ] enforcement at an upload boundary, once one exists
- [x] contributor-level and region-level redaction

Pulled ahead of Phase 4 deliberately. Accumulated observations make a server's generation parameters recoverable, which is irreversible and affects people who never contributed, so the decision has to exist before the ability to publish does. See [`trust-model.md`](trust-model.md).

## Phase 3 — community protocol

**Goal:** allow independent clients to exchange observations without trusting a central database format.

- contributor public keys
- signed observation envelopes
- resumable upload protocol
- object existence negotiation
- server registry and aliases
- collection/landmark metadata
- archive epoch manifests
- mirror-friendly immutable bundles

Exit criterion: two independently operated nodes can exchange, verify, and merge an archive without sharing a database.

## Phase 4 — public archive service

**Goal:** make contribution and historical browsing usable by ordinary players.

- account and contributor identity
- archive ingestion API
- moderation/embargo controls
- server pages
- coverage map
- historical timeline
- observation provenance UI
- conflict inspection
- landmark collections
- web viewer integration

Exit criterion: a player can contribute a capture, see where it increased coverage or corroborated existing data, and browse a historical snapshot without handling raw files.

## Phase 5 — large-server reconstruction

**Goal:** support archives whose raw data is measured in terabytes.

- region/time partitioning
- batch verification
- distributed compaction
- incremental exports
- delta-friendly immutable releases
- torrent/object-mirror publishing
- component-level consensus and transition inference

Exit criterion: archive size and contributor count scale without changing observation identity or requiring a single authoritative storage node.
