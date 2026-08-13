# Architecture

WorldLedger treats capture, preservation, verification, and presentation as separate layers.

## System boundary

```text
                 Minecraft server
                        │
                        ▼
                Capture adapter
             (Fabric / proxy / import)
                        │
             canonical observations
                        │
                        ▼
                 Archive ingress
                        │
          ┌─────────────┴─────────────┐
          ▼                           ▼
 content-addressed objects      observation ledger
          │                           │
          └─────────────┬─────────────┘
                        ▼
             verification / merge
                        │
          ┌─────────────┼─────────────┐
          ▼             ▼             ▼
       exports       web views     community API
```

The core has no dependency on Minecraft packet classes. An adapter is responsible for converting a protocol-specific view into a versioned canonical representation.

## Archive model

The fundamental record is an **Observation**.

An observation binds:

- a server identifier;
- dimension and chunk coordinates;
- the time the state was observed;
- the contributor and capture agent;
- the protocol/canonicalization context;
- one or more content-addressed components.

The payload may initially be coarse (`chunk`) and later become componentized (`terrain`, `biomes`, `block_entities`, `entities`, `containers`). The ledger model supports either without changing object storage.

## Identity

Two hashes serve different purposes.

### State digest

A state digest identifies the ordered set of named component objects. It deliberately excludes contributor and timestamp metadata. Independent observers can therefore report the same state digest.

### Observation id

An observation id includes location, observed time, protocol context, contributor, and state digest. It identifies a particular assertion by a particular source.

`received_at` is excluded because transport and upload delay must not change observation identity.

## Content-addressed storage

Objects are addressed by SHA-256:

```text
objects/
└── sha256/
    └── ab/
        └── cd/
            └── abcdef...
```

This gives immediate deduplication when contributors upload identical canonical state. The current implementation stores objects uncompressed; storage codecs can be added below the digest boundary as long as hashes continue to refer to canonical uncompressed bytes.

## Verification

The first verification primitive is intentionally conservative.

Observations for the same chunk are grouped into a configurable time window. Within that window, observations are grouped by state digest:

- one state, one independent contributor: `single-source`;
- one state, two or more independent contributors: `corroborated`;
- multiple states: `conflict`.

A conflict is never resolved by majority vote in the core. Time uncertainty, world changes, packet ordering, incomplete components, malicious submissions, and capture bugs all require more context than a vote count provides.

Future verification can add signed contributors, capture confidence, clock uncertainty, component-level comparison, and transition inference without invalidating the original observations.

## Server identity

The current CLI accepts an opaque normalized server id. DNS names are useful but insufficient as permanent identity: domains can change ownership and networks can move between addresses.

A public service should introduce a server registry with stable UUIDs and aliases. Archive observations should eventually reference that stable id while retaining the address observed by the capture adapter as provenance.

## Concurrency and scale

The on-disk index is intentionally simple for the first executable core. Local readers and writers are serialized by an operating-system archive lock, so independent importer processes cannot lose one another's index updates. The format remains suitable for local archives and development fixtures, not a public distributed multi-writer service.

The public service should separate immutable object storage from query indexes:

- object payloads: S3-compatible blob storage or filesystem-backed CAS;
- immutable observation metadata: append-only database records;
- query/index layer: PostgreSQL initially;
- large historical datasets: partitioned by server, dimension, region, and time;
- distribution: mirrors/torrents for immutable archive epochs.

The archive format and the service database do not need to be identical. The portable archive remains the interchange boundary.

### Local commit recovery

Observation and chunk-index updates are committed through a small on-disk
transaction record. Opening an archive replays any transaction left by an
interrupted process before serving reads. Observation files and rewritten chunk
indexes are individually replaced atomically, so recovery never has to infer a
record from a truncated append. Immutable CAS objects written before a failed
transaction may remain unreachable and can be reclaimed by a future compactor.
The same exclusive archive lock covers transaction replay, observation/index
commit, indexed reads, and integrity scans.

## Adapter interchange

The first game-facing integration uses a filesystem spool rather than an in-process language binding. Capture adapters produce `worldledger.capture-bundle/v1` directories; the core verifies and imports them. See [`../spec/capture-bundle-v1.md`](../spec/capture-bundle-v1.md) and [ADR 0001](decisions/0001-adapter-boundary.md).

Minecraft Java chunk components are defined independently of Fabric implementation details in [`../spec/minecraft-java-chunk-v1.md`](../spec/minecraft-java-chunk-v1.md). Unknown state is represented by component absence, not by fabricated defaults; see [ADR 0002](decisions/0002-observed-state.md).

The 26.2 Fabric adapter copies bounded semantic snapshots on the client thread, then performs canonical encoding, hashing, manifest generation, fsync, and ready publication on one background writer. A bounded queue makes lost coverage observable instead of allowing disk pressure to grow the heap without limit. Complete temporary bundles are recovered at startup; invalid ones are quarantined rather than published or silently removed.
