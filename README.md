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

## Quickstart

Take `worldledger-desktop` for your platform from the [releases page](https://github.com/TonyLu786/WorldLedger/releases) and run it. It opens a window, checks what your Minecraft is missing, and offers to add Fabric and the mod in one step, naming every file it would write before it writes any of them. After that it is: play, bring it in, decide what may be shared, make a world.

Two things to expect the first time. Windows will warn that the file is from an unknown publisher, because it is not code-signed — More info, then Run anyway, or check its SHA-256 against `SHA256SUMS.txt` on the releases page first. And Minecraft has to have been played once at 26.2 before there is anything to add the mod to; the application says so rather than guessing.

The one thing it will not do for you is the deciding. An archive holds where you went and when, and nothing becomes a world until a named person has said what may happen to it. That is the point of the step, not friction in it.

Everything the window does is also the command line below, which is the way in if you would rather have one, or are scripting, or want the operations the window deliberately leaves out.

## The same path from a terminal

Six steps from nothing to a world you can walk around in.

**1. Install.** Fabric Loader 0.19.3 and Fabric API for Minecraft 26.2, then the mod JAR from the [releases page](https://github.com/TonyLu786/WorldLedger/releases) into `.minecraft/mods`. Take the `worldledger` archive for your platform from the same page and unpack it anywhere.

**2. Turn capture on.** Start the client once, then open `.minecraft/config/worldledger/capture.properties` and put your name in:

```properties
contributor=alice
```

**3. Reload and play.** Back in the client, run `/worldledger reload`, then join a multiplayer server and play normally. `/worldledger status` says whether it is recording and how much it has. Capture never touches the server or the world; it records the chunks the server already sent you.

**4. Import what you captured.** After you disconnect:

```sh
worldledger init ./archive
worldledger ingest-spool --archive ./archive
```

The spool is found under your Minecraft directory automatically, and the command prints which one it used.

**5. Decide what may be shared.** An archive holds where you went and when. Nothing can be exported until someone has said what may happen to it:

```sh
worldledger policy set --archive ./archive --server example.org \
    --disposition private --declared-by alice
```

**6. Make a world.** In Minecraft, create a new empty single-player world and quit to the title screen. Then write your observations into it:

```sh
worldledger export --archive ./archive --server example.org \
    --into .minecraft/saves/<the world you just made>
```

Open that world. The chunks you saw are there; the ones nobody saw are left as the empty world generated them, because an archive that guesses is not an archive.

Every command answers `--help`, and each one ends by naming the next.

## What makes this different from a world downloader

A world downloader saves what one player can see, once, overwriting whatever it saw before. WorldLedger keeps three things such a tool structurally cannot:

- **Time.** Observations are immutable and per-chunk, so an archive can be read at a chosen moment rather than only at its latest state.
- **Provenance and disagreement.** Many contributors can cover one server. When they disagree, both states are kept and labelled, and the selection policy is explicit.
- **Honest unknowns.** A component that was never observed is absent, not defaulted. An export leaves unobserved chunks unwritten instead of filling them with air.

## Install

Prebuilt archives for Windows, Linux, and macOS are on the [releases page](https://github.com/TonyLu786/WorldLedger/releases). Each carries the `worldledger` binary, the committed release profiles, and every document this README links to, so the copy you download is readable offline and none of its links go nowhere. The Fabric mod JAR is published alongside them, and there is exactly one: installing a sources JAR by mistake fails silently. Each release carries a `SHA256SUMS.txt`.

## Capture

Install the mod with the exact Fabric Loader and Fabric API versions in [Supported Fabric baseline](#supported-fabric-baseline). On first client start the adapter creates `<minecraft-config>/worldledger/capture.properties`:

```properties
contributor=alice
server_id=
coalesce_ticks=10
queue_capacity=32
max_snapshots_per_tick=1
```

Set a non-blank contributor and run `/worldledger reload`. Leaving `server_id` blank uses the normalized multiplayer server address; leaving `contributor` blank disables capture. Ready bundles appear under `<minecraft-config>/worldledger/spool/ready-<session-uuid>-<sequence>`.

`coalesce_ticks` and `queue_capacity` are read once when capture starts and are the two settings a reload cannot change; the reload notice says so rather than leaving you to discover it.

| command | |
|---|---|
| `/worldledger` or `/worldledger status` | whether capture is on, under whose name, what this session has taken, and how many bundles are waiting |
| `/worldledger spool` | where the captures are and the command that imports them |
| `/worldledger reload` | re-read `capture.properties` without restarting |

These are client commands. They are handled locally, work on any server including one that has never heard of this mod, and need no permission.

The adapter also speaks up in chat when you join: that capture is off and which file to edit, or that it is running and under whose name. The next join reports what the previous session captured, including anything dropped and where it went, because a disconnect leaves no screen to write to. Single-player is ignored silently, since it is out of scope and saying so every time would be noise.

The adapter is client-only, has no server entrypoint, and does not capture single-player worlds. See [`adapters/fabric/README.md`](adapters/fabric/README.md).

## Use

```sh
worldledger init ./archive
worldledger ingest-spool --archive ./archive
worldledger status --archive ./archive
```

`ingest-spool` finds the spool under the usual Minecraft directory for your platform and prints which one it used. Pass a directory to override it, which is what a second Minecraft installation or a copied spool needs.

`status` answers what the archive holds and what has to happen next: how much was captured, which servers have a publication decision and which do not, and what to run about it. Pass `--spool` as well and it reports how many bundles are still waiting to be taken in.

`ingest-spool` takes in every ready bundle and removes each one once the archive has it on disk, because a spool nothing empties grows until the disk does. Pass `--keep` to leave them, or `--dry-run` to see what would happen. Entries still being written and entries the adapter quarantined are left alone and reported: the first means a client is running, the second is a failure worth looking at.

Importing a real session takes a while, though less than it used to. A captured chunk carries about fifty components, and a component the archive already holds is not written again. A 158-bundle session measured 23 to 25 seconds on one Windows machine, against 2 minutes 25 seconds before those two costs were found.

### Inspect what an archive knows

```sh
worldledger coverage --archive ./archive --server example.org --dimension minecraft:overworld
```

Reports, for a chosen moment, how many chunks are corroborated by independent contributors, how many rest on a single source, and where nothing was observed at all.

Disagreement is split in two, because a Minecraft world is mutable and treating every difference as a contradiction buries the ones that matter. States seen far enough apart are `superseded`: the world changed and the later one is used. States seen close enough together are a `conflict`: contributors disagree about the same moment, and both are kept.

Add `--map coverage.png` to draw it, one pixel per chunk. Chunks with nothing observed are left as background rather than given a colour, so gaps read as absence instead of as another kind of data.

### See what changed between two moments

```sh
worldledger diff --archive ./archive --server example.org --since 24h
```

`coverage` answers what the world looked like at one moment. `diff` answers what happened between two, and separates the chunks it can speak for from the ones it cannot:

| | |
|---|---|
| `changed` | observed on both sides, and the state differs |
| `unchanged` | observed again during the interval, and the state was the same |
| `not revisited` | nobody looked while the interval ran |
| `first seen` | nothing was observed here before the interval |
| `never seen` | observed only after the interval ended |

The distinction between `unchanged` and `not revisited` is the point. A chunk observed once keeps reporting that state forever, so comparing two exports would call it unchanged when what actually happened is that nobody went back. Only the first is a claim about the world; the second is a claim about the archive. A world export cannot express the difference at all, because it has to write some block into every position.

Each changed chunk is listed with who observed the new state and when, and a chunk whose contributors disagree is marked rather than quietly resolved.

Pass `--from` and `--to` for an explicit interval, or `--since 30m` to measure back from the end. With no interval at all the comparison starts at the first observation, which leaves almost everything `first seen`; when nothing could be compared the output says so and prints the range that was actually observed.

Add `--json` for the whole comparison, including every observation made during the interval.

### Name the places you have been

```sh
worldledger landmark set --archive ./archive --server example.org \
    --name spawn --region -2,-2,2,2 --declared-by alice
worldledger coverage --archive ./archive --server example.org
```

A chunk coordinate is not how anybody thinks about where they have been. What the archive could say about a captured session was `157 chunk(s) across 4 region(s), x -6..6, z -6..6`, where a person would say "spawn". With landmarks declared, `coverage` also reports:

```text
landmarks
  far east                 none of its 121 chunk(s)
  spawn                    all 25 chunk(s)
```

Pass `--landmark spawn` to scope a report to one of them.

A landmark is a declaration, not an observation, and that distinction is why it is stored beside publication policies and redactions rather than on a chunk. An observation is evidence that a client saw certain bytes at a certain instant; a landmark is somebody asserting that an area means something. Each one records who declared it, and removing one touches nothing that was observed.

Landmarks are local. A transfer bundle carries observations, and a name somebody chose for a place is not one, so two archives that merged everything they saw still each keep their own names for it.

### Record what a moment looked like

```sh
worldledger epoch --archive ./archive --server example.org --at 2026-08-16T12:00:00Z --out epoch.json
```

An exported world carries no record of where it came from. Two people can export the same server at the same instant from archives holding different observations, get different worlds, and have no way to find out short of comparing region files.

An epoch manifest is that record: every chunk position, the state chosen there, and a root digest over the two. Hand it to anyone exporting the same moment and they run `--compare` against it.

The root covers the positions and the states, and deliberately nothing else. Two archives that selected the same state through different contributors export the same world, so they agree on the root; an archive holding two agreeing observations where another holds one calls the chunk `corroborated` rather than `single-source`, exports the same blocks, and agrees on the root. Those confidence differences are reported separately, because they are worth knowing and are not what makes a world.

`--compare` exits non-zero when the worlds differ, so it can gate a publication step.

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

**One bundle moves data one way**, so between the two directions the archives are deliberately unequal and comparing them lists every chunk the other has not got yet. That is the working midpoint, not a fault. Each command says which one comes next, and `manifest --compare` says which direction would settle what it found.

Two real archives, of 158 and 40 observations, converged this way: after both directions each held 198 and their manifest roots were equal. The second direction moved 40 observations in zero objects, because content addressing had already put the component bytes on both sides. See [`spec/transfer-bundle-v1.md`](spec/transfer-bundle-v1.md).

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
    --target-profile profiles/minecraft-java-1.21.11.json \
    --rules rules.json --on-unrepresentable skip-chunk
```

Writes an approximated copy into a separate world, leaving the faithful export untouched. Conversion is never implicit: it is a different command and refuses to run without an explicit target. Renames preserve state, substitutions are declared by the operator and reported as lossy, and anything the target cannot represent is refused, skipped, or filled according to a policy you choose. Sections outside the target's build range are dropped and counted. See [`docs/version-compatibility.md`](docs/version-compatibility.md).

### Generation research

`worldledger seed` searches for structure placement parameters consistent with structures you supply. It prints a responsibility notice and refuses to run until someone accepts it by name, and that name is written into every result it produces.

It models structure placement only, so what it reports are candidates and structure seeds rather than a world seed. Read [`docs/seed-recovery.md`](docs/seed-recovery.md) for what it does and does not do, and [`docs/trust-model.md`](docs/trust-model.md) for why the exposure this concerns comes from publishing observations at all, not from this tool existing.

## Design rules

1. Observed state is not authoritative server state.
2. Unknown state is not a default value.
3. Conflicts remain first-class data; the core does not vote them away.
4. Canonical uncompressed bytes are hashed before any storage codec.
5. Provenance remains attached to every observation.
6. Capture adapters never write archive internals.
7. Normal client visibility is the collection boundary.

## Build from source

The archive core requires Go 1.23 or newer.

```sh
go test ./...
go vet ./...
go build -trimpath -o bin/worldledger ./cmd/worldledger
go build -trimpath -o bin/mcprofile ./cmd/mcprofile
```

On Windows PowerShell, `scripts\build.ps1` builds both. It uses Go from PATH, and falls back to a toolchain placed beside the checkout under `.tools`, so the documented commands can be followed by name on a machine where Go was never installed system-wide:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build.ps1
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

## Release profiles

A profile records what a Minecraft release can represent: its data version, each dimension's build range, its block and biome registries, and its structure placement parameters. Profiles are extracted from a real game artifact, never hand-written:

`mcprofile` ships in the release archive next to `worldledger`, because this is the step that makes any release usable:

```sh
mcprofile --jar <client.jar> --out profiles/minecraft-java-<version>.json
```

Two are committed, `profiles/minecraft-java-26.2.json` and `profiles/minecraft-java-1.21.11.json`, so the conversion path is exercised against a release that is genuinely smaller rather than a synthetic one.

Two profiles can be compared, which is what a Minecraft upgrade needs. The comparison separates what a release merely adds from what it stops representing, moves, or re-salts, because only the second kind bears on observations already captured:

```sh
mcprofile --from profiles/minecraft-java-1.21.11.json --to profiles/minecraft-java-26.2.json
```

From a source checkout, each of these is `go run ./cmd/mcprofile` with the same flags. `dfurenames` extracts Mojang's own rename tables from the compiled data fixers, reporting which fixers it could not read rather than implying full coverage; it needs `javap` and a Mojang-mapped jar, so it is run from a checkout rather than shipped:

```sh
go run ./cmd/dfurenames --jar <mojang-mapped.jar> --source <version> --out profiles/renames-<version>.json
```

See [`docs/upgrading-minecraft.md`](docs/upgrading-minecraft.md) for what to run when a release lands and how to tell a game change from a regression.

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
