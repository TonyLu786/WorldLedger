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
- [x] cross-platform digest comparison

The integration fixture is now an automated client game test rather than only a written procedure. It passed against a real 26.2 client on Windows: 158 ready bundles, none dropped, all imported, idempotent on repeat, `fsck` clean. The same test runs headless in Linux CI on every push.

The two have now been compared. A Windows capture and a Linux capture of the same pinned world produced byte-identical fingerprints across all 157 chunks both observed. The reference is committed, so a future divergence fails the build instead of waiting to be noticed.

Exit criterion: **met.** A live multiplayer session produced a deterministic local archive that then exported to a playable world, and two platforms canonicalized the same observed state identically. See [`status.md`](status.md).

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
- [x] converted world opened in an older release

`level.dat` is deliberately not generated. It carries the data version, generator, and build height, so an export is written into a world the target client created rather than into one this project invented. This is a decision, not a missing feature.

The older release is real and so is the run against it. `profiles/minecraft-java-1.21.11.json` is extracted from Mojang's 1.21.11 jar, the loss rules are tested against a registry that is genuinely smaller rather than a synthetic one, and a converted world has been opened by a 1.21.11 server, which answered `execute if block` correctly at five coordinates and rewrote the chunks from its own world model. See [`status.md`](status.md).

Exit criterion: **met for the faithful export path, and for conversion on a server.** An exported chunk loads and renders correctly in an unmodified Minecraft 26.2 client, including negative sections and block state properties, verified in game and by an independent reader. A converted chunk survives a round trip through a 1.21.11 server with its blocks at the same coordinates. What conversion still lacks is a client: nothing has looked at one.

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

- [x] contributor public keys
- [x] signed observation envelopes
- [x] object existence negotiation
- [ ] resumable upload protocol
- [ ] server registry and aliases
- [x] collection/landmark metadata
- [x] archive epoch manifests
- [x] mirror-friendly immutable bundles

The four that are done are the ones that do not need a network service, and they are the ones the rest depends on. A contributor label used to be a string an adapter wrote; an attestation is now an ed25519 signature over the observation id, which already digests the server, dimension, chunk, instant, protocol, contributor, and state, so a signature cannot be moved to another record. Object existence negotiation falls out of content addressing: two mirrors work out what to send from digests alone, with neither opening the other's archive.

What signing does not do is worth repeating here. It proves a key asserted something. It does not make the assertion true, and nothing stops someone generating a key and picking a name, which is why the identity registry is explicit, attributed, and refuses to let a second key take a label already held. Sybil contributors remain a threat this cannot solve alone.

Transfer bundles carry the result. A bundle is an ordinary directory that can be copied by any means, and the receiver verifies every byte against the digest the bundle declares rather than trusting where it came from. Two real archives holding 158 and 40 observations were merged this way in both directions and ended on the same manifest root, having never shared a database.

An epoch manifest is what an archive says the world was at one moment: every chunk position, the state chosen there, and a root digest over the two. It closes a gap an exported world leaves open, since a world carries no record of the archive, instant or policy that produced it. Its root covers positions and states and nothing else, so two archives that would export the same world agree on one value whoever observed it and however well attested it is; differences in confidence are reported apart from differences in the world.

A landmark names an area of one dimension of one server, so coverage can report "all 25 chunks of spawn" rather than a range of coordinates. It is a declaration rather than an observation and is stored with the publication policies for that reason: an observation is evidence, a landmark is an assertion, and letting the second be mistaken for the first would put an opinion in the record of what was seen. Landmarks stay local, because a transfer bundle carries observations and a name somebody chose is not one.

The two that remain need a service to exist first. A resumable upload protocol has nothing to resume against, and a server registry with no servers registering is a schema rather than a feature.

Exit criterion: **met for offline exchange.** Two independently operated archives exchanged, verified, and merged over a directory. Doing it over a network is what the remaining items are for.

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
