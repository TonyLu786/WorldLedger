# WorldLedger

WorldLedger saves the parts of a multiplayer Minecraft server you have actually seen while playing, and turns them back into a world you can open in single player.

It is two pieces: a client-only Fabric mod for Minecraft 26.2 that records chunks as they arrive, and a `worldledger` command-line tool that assembles those recordings and writes Anvil region files.

A world downloader keeps one snapshot and overwrites it. WorldLedger keeps every observation with the moment and the person it came from, so an archive can be read at a chosen point in time, and two players who explored the same server can merge what they saw without either one overwriting the other.

Underneath, it stores canonical component bytes in content-addressed storage and preserves each contributor's observation, including conflicting observations, as immutable evidence.

The repository contains two deliberately separate systems:

- a Go archive core and `worldledger` command-line interface;
- a client-only Fabric adapter that writes local `worldledger.capture-bundle/v1` spool entries.

The Fabric process never opens or mutates an archive. The Go importer is the only boundary that turns a capture bundle into archive objects, observation records, and chunk indexes.

> **Development status.** The archive core, canonical encoders and decoders, epoch selection, Anvil export, release profiles, and publication policy are implemented and automatically tested. Reconstruction has been verified end to end against an unmodified Minecraft 26.2 client: an exported chunk loads and renders correctly, including negative sections and block state properties. Capture has been exercised against a real 26.2 client as well, and the client game test runs headless in Linux CI on every push. A Windows capture and a Linux capture of the same pinned world have been compared and canonicalized to byte-identical results, so the encoding is platform independent by measurement rather than by construction; that reference is committed and CI fails on a future divergence. See [`docs/status.md`](docs/status.md) for every claim and the evidence behind it, and [`CHANGELOG.md`](CHANGELOG.md) for what a given build contains.

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

## Install

Prebuilt archives for Windows, Linux, and macOS are on the [releases page](https://github.com/TonyLu786/WorldLedger/releases). Each carries the `worldledger` binary, the committed release profiles, and every document this README links to, so the copy you download is readable offline and none of its links go nowhere. The Fabric mod JAR is published alongside them, and there is exactly one: installing a sources JAR by mistake fails silently. Every file has a `.sha256` beside it.

## Build from source

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
queue_capacity=32
max_snapshots_per_tick=1
```

Set a non-blank contributor and restart the client. Leaving `server_id` blank uses the normalized multiplayer server address; leaving `contributor` blank disables capture. Ready bundles appear under `<minecraft-config>/worldledger/spool/ready-<session-uuid>-<sequence>`.

The adapter says what it is doing in chat when you join a multiplayer server: that capture is off and which file to edit, or that it is running and under whose name. The next join reports what the previous session captured, including anything dropped, because a disconnect leaves no screen to write to. Single-player is ignored silently, since it is out of scope and saying so every time would be noise.

The adapter is client-only, has no server entrypoint, and does not capture single-player worlds. See [`adapters/fabric/README.md`](adapters/fabric/README.md).

## Use

```sh
worldledger init ./archive
worldledger ingest-spool --archive ./archive <minecraft-config>/worldledger/spool
worldledger status --archive ./archive
```

`status` answers what the archive holds and what has to happen next: how much was captured, which servers have a publication decision and which do not, and what to run about it. Pass `--spool` as well and it reports how many bundles are still waiting to be taken in.

`ingest-spool` takes in every ready bundle and removes each one once the archive has it on disk, because a spool nothing empties grows until the disk does. Pass `--keep` to leave them, or `--dry-run` to see what would happen. Entries still being written and entries the adapter quarantined are left alone and reported: the first means a client is running, the second is a failure worth looking at.

Importing a real session takes a while. A captured chunk carries about fifty components and each one is put on disk durably before the import is acknowledged, which measured around 918 ms per bundle on one Windows machine.

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

### Compare two captures

```sh
worldledger fingerprint --archive ./archive --out capture.txt
worldledger fingerprint --archive ./other --compare capture.txt
```

A manifest digests observation identities, and an identity carries the instant and the session that produced it, so two captures of the same world always disagree there. A fingerprint carries state and component digests only. Two machines that observed the same states agree exactly, whatever else differed.

The comparison separates three things, because running them together hides the one that matters: chunks only one capture saw, chunks where one capture caught a change the other missed, and chunks where each side holds a state the other cannot account for. Only the last indicates a defect.

### Declare a publication policy

Every server needs one explicit, attributed decision before the archive will build a world from it:

```sh
worldledger policy set --archive ./archive --server example.org \
    --disposition private --declared-by your-name --note "why"
worldledger policy list --archive ./archive
```

Dispositions are `private`, `embargoed` (with `--until`), `research`, and `public`. An undeclared server is treated as an unanswered question, not as permission. See [`docs/trust-model.md`](docs/trust-model.md) for why accumulated coverage, rather than any single observation, is what this guards.

### Sign what you contributed

A contributor label is a string an adapter wrote. Anyone can put any name there, which is fine while an archive is your own files and useless the moment two parties exchange observations.

```sh
worldledger identity create --archive ./archive --label alice \
    --declared-by your-name --key-out alice.key
worldledger attest sign --archive ./archive --key alice.key
worldledger attest verify --archive ./archive
```

The signature covers the observation id, which already digests the server, dimension, chunk, instant, protocol, contributor label, and state, so it cannot be moved to another record. Signing only touches observations already attributed to that label: vouching for someone else's record is a different act, and mixing the two would make a signature mean less than nothing.

To recognise a contributor whose observations arrived from elsewhere, register their public key:

```sh
worldledger identity register --archive ./archive --label bob \
    --public-key <hex> --declared-by your-name --note "how you checked"
```

A signature proves a key asserted something. It does not make the assertion true, and nothing stops someone generating a key and picking a name. The registry is where that judgment lives: it is attributed, and it refuses to let a second key take a label another key already holds. An unsigned observation is not suspect, it is simply a claim nothing backs.

### Work out what two mirrors need to exchange

```sh
worldledger fingerprint --file ours.txt --negotiate theirs.txt
```

Objects are addressed by content, so deciding what to transfer is a set difference over digests. Neither side opens the other's archive and no chunk data moves in order to decide what would move.

### Exchange with another archive

```sh
# they send you their fingerprint and manifest
worldledger send --archive ./archive --to theirs.txt --their-manifest theirs.json --out ./outbound
# they run this on the directory you hand them
worldledger receive --archive ./their-archive ./outbound
```

A transfer bundle is an ordinary directory: copy it however you like. The receiver verifies every object against the digest the bundle declares and recomputes every observation's identity, so a bundle from an untrusted peer cannot introduce anything the archive would not have accepted from its own adapter. Importing the same bundle twice changes nothing.

One bundle moves data one way. Two archives converge once each has sent to the other, at which point their manifest roots are equal. See [`spec/transfer-bundle-v1.md`](spec/transfer-bundle-v1.md).

### Withhold observations

A contributor may withdraw consent, or an operator may ask for one area to be excluded whoever observed it. Both are declared, attributed, and reversible:

```sh
worldledger redact set --archive ./archive --server example.org \
    --contributor alice --reason "contributor withdrew consent" --declared-by your-name
worldledger redact set --archive ./archive --server example.org \
    --region -2,-2,2,2 --reason "operator asked for the spawn area to be excluded" --declared-by your-name
worldledger redact list --archive ./archive
```

Declared redactions are withheld from coverage, export, and convert immediately. `inspect`, `fsck`, and `fingerprint` still see everything: an operator examining their own archive is not what this guards, and a diagnostic that hides data is a diagnostic that lies.

Removing the data is a separate, irreversible step, and it will not always remove much:

```sh
worldledger redact purge --archive ./archive        # describes what would go
worldledger redact purge --archive ./archive --yes  # carries it out
```

Objects are addressed by content, so two contributors who observed the same chunk in the same state share one object. Purging reports every object it could not remove and names the surviving contributor who still references it. On the first real two-contributor archive measured here, withdrawing one contributor removed 40 observations and zero bytes, because every state they had seen had been independently observed by someone else. An archive that claimed otherwise would be lying to the person who asked to be forgotten.

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
- [`spec/transfer-bundle-v1.md`](spec/transfer-bundle-v1.md)

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
