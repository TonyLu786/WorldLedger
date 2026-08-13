# WorldLedger

WorldLedger is a community-maintained ledger of client-observable multiplayer Minecraft world state. It stores canonical component bytes in content-addressed storage and preserves each contributor's observation, including conflicting observations, as immutable evidence.

The repository contains two deliberately separate systems:

- a Go archive core and `worldledger` command-line interface;
- a client-only Fabric adapter that writes local `worldledger.capture-bundle/v1` spool entries.

The Fabric process never opens or mutates an archive. The Go importer is the only boundary that turns a capture bundle into archive objects, observation records, and chunk indexes.

> **Development status.** The archive core, canonical encoders and decoders, epoch selection, Anvil export, release profiles, and publication policy are implemented and automatically tested. Reconstruction has been verified end to end against an unmodified Minecraft 26.2 client: an exported chunk loads and renders correctly, including negative sections and block state properties. Capture has been exercised against a real 26.2 client as well, and the client game test runs headless in Linux CI on every push. What has not been done is a digest comparison between captures taken on different platforms, so the Fabric adapter remains `0.1.0-dev`. See [`docs/status.md`](docs/status.md).

## What makes this different from a world downloader

A world downloader saves what one player can see, once, overwriting whatever it saw before. WorldLedger keeps three things such a tool structurally cannot:

- **Time.** Observations are immutable and per-chunk, so an archive can be read at a chosen moment rather than only at its latest state.
- **Provenance and disagreement.** Many contributors can cover one server. When they disagree, both states are kept and labelled, and the selection policy is explicit.
- **Honest unknowns.** A component that was never observed is absent, not defaulted. An export leaves unobserved chunks unwritten instead of filling them with air.

## Design rules

1. Observed state is not authoritative server state.
2. Unknown state is not a default value.
3. Conflicts remain first-class data; the core does not vote them away.
4. Canonical uncompressed bytes are hashed before any storage codec.
5. Provenance remains attached to every observation.
6. Capture adapters never write archive internals.
7. Normal client visibility is the collection boundary.

## Install and build

The archive core requires Go 1.23 or newer.

```sh
go test ./...
go vet ./...
go build -trimpath -o bin/worldledger ./cmd/worldledger
```

On Windows PowerShell:

```powershell
go build -trimpath -o .\bin\worldledger.exe .\cmd\worldledger
```

The Fabric adapter requires a Java 25 JDK:

```sh
cd adapters/fabric
./gradlew clean build --warning-mode all
```

The installable mod JAR is written to `adapters/fabric/build/libs/worldledger-fabric-0.1.0-dev.jar`.

### Supported Fabric baseline

```text
Minecraft      26.2          (data version 4903)
Java           25
Fabric Loader  0.19.3
Fabric API     0.156.0+26.2
Fabric Loom    1.17.17
Gradle         9.7.0 (wrapper, checksum-pinned)
Names          native Mojang names used by 26.2
```

All values are exact build inputs in [`adapters/fabric/gradle.properties`](adapters/fabric/gradle.properties) and the Gradle wrapper configuration.

## Capture

Install the built mod with the exact Fabric Loader and Fabric API versions above. On first client start the adapter creates `<minecraft-config>/worldledger/capture.properties`:

```properties
contributor=alice
server_id=
coalesce_ticks=10
queue_capacity=8
max_snapshots_per_tick=1
```

Set a non-blank contributor and restart the client. Leaving `server_id` blank uses the normalized multiplayer server address; leaving `contributor` blank disables capture. Ready bundles appear under `<minecraft-config>/worldledger/spool/ready-<session-uuid>-<sequence>`.

The adapter is client-only, has no server entrypoint, and does not capture single-player worlds. See [`adapters/fabric/README.md`](adapters/fabric/README.md).

## Use

```sh
worldledger init ./archive
worldledger ingest-bundle --archive ./archive ./spool/ready-session-sequence
worldledger fsck --archive ./archive
```

### Inspect what an archive knows

```sh
worldledger coverage --archive ./archive --server example.org --dimension minecraft:overworld
```

Reports, for a chosen moment, how many chunks are corroborated by independent contributors, how many rest on a single source, and where nothing was observed at all.

Disagreement is split in two, because a Minecraft world is mutable and treating every difference as a contradiction buries the ones that matter. States seen far enough apart are `superseded`: the world changed and the later one is used. States seen close enough together are a `conflict`: contributors disagree about the same moment, and both are kept.

Add `--map coverage.png` to draw it, one pixel per chunk. Chunks with nothing observed are left as background rather than given a colour, so gaps read as absence instead of as another kind of data.

### Compare two archives

```sh
worldledger manifest --archive ./archive --out manifest.json
worldledger manifest --archive ./other --compare manifest.json
```

A manifest carries per-chunk digests, so two mirrors agree or disagree on a single root value and can localise every difference to an individual chunk without transferring any chunk data.

### Declare a publication policy

Every server needs one explicit, attributed decision before the archive will build a world from it:

```sh
worldledger policy set --archive ./archive --server example.org \
    --disposition private --declared-by your-name --note "why"
worldledger policy list --archive ./archive
```

Dispositions are `private`, `embargoed` (with `--until`), `research`, and `public`. An undeclared server is treated as an unanswered question, not as permission. See [`docs/trust-model.md`](docs/trust-model.md) for why accumulated coverage, rather than any single observation, is what this guards.

### Export a world

```sh
worldledger export --archive ./archive --server example.org --into /path/to/world
```

Writes the observed state unchanged into a world you created in the target release. It does not create the world: `level.dat` carries the data version, generator, and build height, and fabricating those is how an export ends up silently upgraded or misaligned. Chunks that were never observed are left unwritten rather than filled in.

Minecraft migrates an older world forward on its own, so a newer client reads a faithful export directly. There is no path backwards, which is what `convert` is for.

### Convert for an older release

```sh
worldledger convert --archive ./archive --server example.org --into /path/to/other-world \
    --target-profile profiles/minecraft-java-26.2.json \
    --rules rules.json --on-unrepresentable skip-chunk
```

Writes an approximated copy into a separate world, leaving the faithful export untouched. Conversion is never implicit: it is a different command and refuses to run without an explicit target. Renames preserve state, substitutions are declared by the operator and reported as lossy, and anything the target cannot represent is refused, skipped, or filled according to a policy you choose. Sections outside the target's build range are dropped and counted. See [`docs/version-compatibility.md`](docs/version-compatibility.md).

### Generation research

`worldledger seed` searches for structure placement parameters consistent with structures you supply. It prints a responsibility notice and refuses to run until someone accepts it by name, and that name is written into every result it produces.

It models structure placement only, so what it reports are candidates and structure seeds rather than a world seed. Read [`docs/seed-recovery.md`](docs/seed-recovery.md) for what it does and does not do, and [`docs/trust-model.md`](docs/trust-model.md) for why the exposure this concerns comes from publishing observations at all, not from this tool existing.

## Release profiles

A profile records what a Minecraft release can represent: its data version, each dimension's build range, its block and biome registries, and its structure placement parameters. Profiles are extracted from a real game artifact, never hand-written:

```sh
go run ./cmd/mcprofile --jar <client.jar> --out profiles/minecraft-java-<version>.json
go run ./cmd/dfurenames --jar <mojang-mapped.jar> --source <version> --out profiles/renames-<version>.json
```

The committed profile for 26.2 is `profiles/minecraft-java-26.2.json`. `dfurenames` additionally extracts Mojang's own rename tables from the compiled data fixers, reporting which fixers it could not read rather than implying full coverage.

## Architecture

```text
multiplayer server
       ↓ legitimate client-visible state
Fabric 26.2 adapter
       ↓ canonical bytes
crash-safe capture-bundle spool
       ↓ verified import
Go content-addressed archive
       ↓ epoch selection under an explicit policy
inspection, coverage, conflict reporting, world export
```

The core never depends on Minecraft packet classes. Adapters absorb protocol churn and produce versioned canonical bytes. Component absence means unknown; it is never silently converted into air, an empty biome, or an empty block entity.

Interchange specifications:

- [`spec/observation-v1.md`](spec/observation-v1.md)
- [`spec/capture-bundle-v1.md`](spec/capture-bundle-v1.md)
- [`spec/minecraft-java-chunk-v1.md`](spec/minecraft-java-chunk-v1.md)

## Repository layout

```text
cmd/worldledger/       archive CLI
cmd/mcprofile/         release profile extractor
cmd/dfurenames/        data fixer rename table extractor
cmd/mcjava-fixtures/   committed-fixture validator
cmd/visualfixture/     capture bundle for visual reconstruction checks
internal/archive/      observation commit, recovery, indexes, enumeration, fsck
internal/bundle/       capture-bundle parser and importer
internal/cas/          content-addressed object storage
internal/mcjava/       canonical encoder and decoder
internal/model/        archive data model and identity rules
internal/verify/       corroboration and conflict analysis
internal/epoch/        point-in-time selection under an explicit merge policy
internal/anvil/        Anvil region and chunk writer
internal/translate/    cross-release block and biome translation
internal/mcprofile/    release capability profiles
internal/policy/       publication policy and coverage exposure assessment
internal/seed/         structure placement, for generation research
adapters/fabric/       Java 25 Fabric 26.2 capture adapter
profiles/              extracted release profiles and rename tables
spec/                  stable interchange specifications
docs/                  architecture, trust model, decisions, roadmap, status
testdata/              immutable golden and Java-to-Go fixtures
examples/              reproducible integration-world procedure
```

## Contributing and security

Changes to identity preimages, canonical bytes, archive layout, or merge semantics require an explicit versioning and migration discussion. See [`CONTRIBUTING.md`](CONTRIBUTING.md), [`SECURITY.md`](SECURITY.md), and [`docs/trust-model.md`](docs/trust-model.md).

## Licence and disclaimer

WorldLedger is licensed under Apache-2.0. See [`LICENSE`](LICENSE).

This software is provided as is, without warranty of any kind. Responsibility for use rests entirely with the user, including obtaining any rights or permissions that use requires and complying with all applicable laws and with the terms of any system or service involved. Nothing here grants permission that the user does not already independently hold. Please read [`NOTICE`](NOTICE) in full before use.

Minecraft is a trademark of Microsoft Corporation. This independent project is not affiliated with Mojang Studios or Microsoft.
