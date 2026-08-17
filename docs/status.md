# Status

What has actually been verified, and by what evidence. Anything not listed as verified should be read as not verified.

## Verified against real Minecraft

**Reconstruction: archive to a playable world.**

An archive was exported into a world created by an unmodified Minecraft 26.2 client, and the result loaded and rendered correctly. Evidence:

- The client's own debug screen reported reading the exported region file.
- A fixture arranged so that each class of error looks different from the outside was checked in game: a red line along +X and a blue line along +Z would swap if the horizontal axes were transposed; three oak logs with `axis=x`, `axis=y`, `axis=z` would look alike if block state properties were lost; a second floor at y=-64 would vanish if negative section coordinates were mishandled. All were correct.
- The chunk at section Y=-4 loaded and rendered, so the negative-height path introduced in 1.18 is exercised.
- An independently written Anvil reader, implemented from the format specification rather than from the writer, unpacked the palette and long array and confirmed twelve specific blocks at their expected coordinates in the file inside the world.

This run also found a real defect that no byte-level test could: Minecraft 26.2 stores every dimension under `dimensions/<namespace>/<path>/region/`, including the vanilla three, and there is no top-level `region/` directory. The exporter had been writing to the older layout, which the game silently ignores.

The capture game test is the standing form of this check. It passes from a clean run: 158 chunks enqueued, none dropped, no snapshot failures, 158 ready bundles, and no quarantined or partial entries once the writer drained. All 158 import, `fsck` reports zero errors, and the archive resolves them to 52 stored objects.

**Cross-release conversion: a converted world opened by an older server.**

A capture taken from 26.2 was converted for 1.21.11 and opened by Mojang's 1.21.11 server, whose SHA-1 matched the one their version manifest publishes. The server was then asked what it had found. Evidence:

- The target is a real release rather than a guess. `profiles/minecraft-java-1.21.11.json` is extracted by `cmd/mcprofile` from that release's own jar, and against 26.2 it is genuinely smaller: 1,168 blocks to 1,198 and 65 biomes to 66. The thirty it lacks are the cinnabar family, the sulfur family, and the golden dandelion in both its planted and potted forms; the biome it lacks is `minecraft:sulfur_caves`. It holds nothing 26.2 does not.
- Converting the 158-observation capture wrote 157 chunks into four region files under `world/region/`, which is the layout 1.21.11 uses and not the one 26.2 uses. The exporter chose it by asking the world it was writing into. A reader that never touches the Go writer reports data version 4903 on the faithful export and 4671 on the converted copy.
- The server loaded that world and logged no chunk error.
- Over RCON, `execute if block` was run at five coordinates: oak log at 2 65 1, oak stairs at 4 65 1, stone at 0 64 0, grass block at 0 -61 0, bedrock at 0 -64 0. All five reported `Test passed`. The same test for a block that is not at 2 65 1 reported `Test failed`, so the check distinguishes rather than passing whatever it is given.
- The server shut down reporting all chunks saved, and it had rewritten the chunk: the palette came back in a different order, which is the signature of Minecraft re-serialising from its own world model rather than leaving the file alone. Every block was still at the same coordinate, and the chunk still carried data version 4671 and status `minecraft:full`.

The coordinates were not chosen by hand. An independently written Anvil reader unpacked the palette and long array from the converted file and reported where each block sat, so the question put to Minecraft was formed from the bytes rather than from the code that produced them.

Two limits belong with this. The conversion of that particular world reported no loss, which is the truth for it: it is superflat and uses none of the thirty blocks 1.21.11 lacks. The loss paths are covered by tests against the same profile, not by this run. And this was a server, so nothing here says how the world renders.

**The whole path, once, by one person, on a public server.**

Somebody installed through the desktop application, played on `top.earthmc.net`, and came back. Nothing was staged: the server is a real one they chose, the session is however long they played, and the archive is the one the application keeps.

- The capture folder held 608 ready bundles and nothing quarantined or half-written.
- All 608 imported, none failed, in three and a half minutes. The archive came out at 608 observations, 4,115 objects, 33.1 MB, over 395 chunks of one server.
- The recordings were left in the capture folder, and the screen said so rather than leaving somebody to wonder whether they had been consumed.
- A disposition was declared by name before anything could be exported. It was refused until then.
- 395 chunks were written into a world, landing in `dimensions/minecraft/overworld/region/` — the 26.2 layout, chosen by asking the world rather than assuming, which is the defect a real client found once before.
- The two region files written hold 380 and 15 chunks, which is the 395 the export reported. The four region files that world already had were left with their 3,540 chunks untouched.
- An independently written Anvil reader, which never touches the Go writer, read chunk (712,320) and (713,319) out of those files: data version 4903, status `minecraft:full`, and real terrain — stone, grass block, water, deepslate — at coordinates around x 11,400 and z 5,120, consistent with the region numbering.

Nothing was observed twice and nothing was withheld, so this run exercises the faithful path rather than the conflict or redaction ones. The world written into was a copy of one the player already had, because a world's seed and rules are never invented; their two existing worlds were not touched.

**The desktop application, as far as it has been run.**

The window opens. Built for Windows and launched on a machine with the WebView2 runtime, it shows a native window titled WorldLedger, and closing that window ends the process. Built with `-H=windowsgui` it shows one window rather than a console behind its own. In browser mode, a page that stops reporting in ends the process after the timeout, which was checked with a shortened timer: without it a closed tab would leave the program running with no way for its owner to stop it.

The path was walked end to end in a browser against forty real capture bundles, on a fabricated Minecraft directory: the health screen read the machine, forty recordings were found and imported, a disposition was declared, forty places were written into a world, and the time-travel map drew. The archive core was called directly throughout; nothing parses command output.

**The installer, against a real Minecraft.**

It was run against an unmodified Minecraft installation with no mods and no config directory, with the mod jar rebuilt from source first. Evidence:

- The five steps it listed beforehand are the five files it wrote, and no others: the Fabric version profile, an entry in the launcher's own list, Fabric API, the mod, and `capture.properties`.
- Fabric API arrived as a real archive of 53 entries and 2,531,175 bytes rather than an error page, and the mod jar's SHA-256 matches the jar that was built.
- The whole of it took a quarter of a second, because no installer program is run: Fabric publishes the version profile the launcher needs, so this is two small downloads and four writes.
- The health check then reported ready, with all six lines green, including the contributor it had written.
- The launcher's own two installations were still there afterwards, alongside the added one, and the rest of that file was intact.
- Uninstalling put everything back. Every file it wrote is gone; the four directories it created are gone; `launcher_profiles.json` is byte-for-byte what it was before; and a SHA-256 of every file two levels deep under `.minecraft` is identical to the same list taken before installing.

What this run does not cover: the game was not started afterwards, so the mod is verified as installed rather than as loading. The capture game test is the standing check for whether the adapter runs, and it runs on every push.

## Verified automatically

- **Canonical identity.** Observation ids, state digests, and object hashes have committed golden vectors. Changing one requires a specification-level explanation. The identity preimage encodes an instant as integer seconds and nanoseconds rather than as formatted text, because text leaves trailing zeros in the fractional part to each language's convention and two conforming implementations would otherwise disagree; golden vectors cover the values where those conventions differ.
- **Point-in-time selection.** Corroboration, supersession, and conflict are distinguished by a simultaneity window, and selection is order independent. Exercised on live capture data, not only on fixtures.
- **Archive manifests.** Two archives compare by a root digest and localise their differences to individual chunks without transferring any chunk data.
- **Cross-language canonicalization.** The Java adapter and the Go reference encoder agree byte for byte on committed fixtures.
- **Canonical decoding.** Every committed golden component decodes and re-encodes to identical bytes. Decoding validates canonical form, so a successful decode is also proof the stored bytes are canonical. Five fuzz targets exercise this against arbitrary input.
- **Bundle ingress.** Hostile bundles are rejected: wrong digests, wrong sizes, path traversal, symlink escape, truncated manifests, oversized components. `fsck` passes after any failed import.
- **Anvil output.** Bit packing, Java modified UTF-8 including supplementary characters, region layout, sector alignment, and reproducibility have exact byte-level assertions.
- **Epoch selection.** The corroborated-first policy is total and order-independent; repeated submissions from one contributor cannot manufacture corroboration.
- **Structure placement.** `internal/seed` is checked against vectors generated by a real JVM running `java.util.Random`.
- **Release profiles.** The committed 26.2 profile is checked against values independently readable from the game artifact.
- **Attestation.** An ed25519 signature over an observation id verifies, and fails when moved to another observation, when the signature is altered, when the key is swapped, or when it was made without the domain separator. The archive refuses to store an attestation that does not verify. A valid signature from an unregistered key is reported as valid and unrecognised rather than as an endorsement, and a second key cannot register a label another key already holds.
- **Object existence negotiation.** Two mirrors work out what to transfer from their fingerprints alone, in both directions, without either opening the other's archive.
- **Archive exchange.** Two archives that never shared a database converge to the same manifest root by exchanging transfer bundles in both directions, exercised on the two real capture sessions on disk. A bundle whose object bytes were substituted is refused, and so is one whose observation was reattributed to another contributor, because the identity no longer matches the record. A repeated import changes nothing.
- **Cross-platform digest agreement.** The same observed world state, captured by a Windows client here and by a Linux client in CI, canonicalized to identical bytes. The two fingerprints agree on all 157 chunks both observed, with no chunk seen by only one side and no state either could not account for, and the two files are byte-identical at 24,677 bytes. The game test pins the world seed, generator and view distance so that a difference between the two could only have come from the encoder. That reference is committed, so every CI run now compares against it and fails on a disagreement rather than reporting one.
- **Spool storage.** Identical component bytes are stored once. Twenty bundles declaring 199,671 bytes occupied 36,879 on disk, identical components resolved to a single file, and deleting one bundle after import left a bundle sharing its bytes readable. A 40 KB budget stopped capture after 32 bundles and left all 32 in place; the same writer with a large budget kept going.
- **Player-facing notices.** The text shown in game is built with no Minecraft type in it and is asserted directly: the disabled notice names the file and the setting, a clean session does not mention drops, a lossy one names them separately from the total, and a session that captured nothing does not read like capture being switched off. The class that draws them holds no logic.
- **In-game commands.** The client game test parses `/worldledger`, `/worldledger status`, `/worldledger spool` and `/worldledger reload` against the live client dispatcher and requires each to reach something executable, then sends one through the client's own command path the way a keystroke would. Registering without throwing is not the same as a command a player can type, and the two fail separately: a tree of the wrong shape parses nothing, and a tree of the right shape can still have a handler that throws on its first line.
- **Redaction.** Contributor and region scopes withhold matching observations from everything the archive builds for sharing. Purging removes observation records, removes objects nothing else references, and reports every object it kept along with the surviving contributor that still needs it. An interrupted purge is journalled and finished when the archive is next opened, because either half-done order leaves a state the integrity check rejects.

CI runs the Go gates on Linux and separately tests, vets, and builds the Windows-specific filesystem and locking paths. The Fabric build runs in Linux CI.

**Capture: multiplayer session to archive.**

The client game test at `adapters/fabric/src/gametest/` passed against a real 26.2 client. It starts a dedicated server, connects a real client, applies block and biome changes, disconnects, and verifies the published bundles: manifest schema, dimension, component presence, and every component's declared size and digest against the bytes on disk. It fails when no bundle appears, so a run with capture disabled cannot pass quietly.

The run produces ready bundles with no quarantined or temporary entries left behind, all of which import into a Go archive; a repeated import is idempotent and `fsck` reports zero errors. Exporting that archive produces a world.

That closes the loop end to end: a live multiplayer session becomes an archive, and the archive becomes a world.

**It also found a defect that no unit test could.** A disconnect releases every dirty chunk at once, and the first run discarded 108 of 158 chunk snapshots because the bounded queue refused what it could not hold. The bound exists so game threads never wait on disk, which is right during play but inverted at disconnect: there is no gameplay left to protect, and refusing a job only destroys observed state. The final flush now waits for the writer under a single ten-second budget shared by the whole flush, so memory stays bounded and leaving a server stays bounded, while nothing observed is thrown away.

After the fix the same test enqueued 158 chunks and dropped none, while the in-play retry path was unchanged. The 158 observations resolve to 52 stored objects: repeatedly snapshotting an unchanged chunk stores nothing new, so content addressing removed about two thirds of the bytes on real capture data.

Evidence for both runs is under `validation/gametest-evidence-2026-08-13*/`.

The same game test runs in Linux CI on every push, headless under a software renderer, and passes there. Because the test fails when no bundle appears, a passing run is evidence that capture produced and verified bundles on that platform, not merely that the client started.

## Measured on the client

**What capture costs the thread that draws frames.** This sat under "not verified" while nobody had measured it. It has been, repeatedly, and the figures below are what a real session produced. The first one was not comfortable:

```text
169 ticks, mean 1080.1 us, max 15216.3 us (30.433% of a 50 ms tick)
```

The same session enqueued 158 chunks, dropped none, and failed on none.

The mean is unremarkable: a millisecond on the ticks that did work. The maximum is not. A frame at 60 fps has 16.7 ms, and the worst tick spent 15.2 ms copying one chunk's state on the thread that draws. That is a dropped frame, and calling it 30% of a tick understates it, because a tick is not the budget a player perceives.

What it is bounded by is already right: `max_snapshots_per_tick` is 1, so the worst case is one full-height chunk, which is 24 sections of 4,096 block states.

There turned out to be a third option besides spreading a chunk over several ticks, which a torn copy would break, and capturing less than a chunk, which loses coverage: ask fewer questions for the same answer. Most of a chunk is one state repeated, and Minecraft stores such a section with a single-value palette, which the client can recognise in constant time. Reading 4,096 positions to be told the same thing 4,096 times is work with no result.

Measured outside the game on real `PalettedContainer`s, over a chunk of 24 sections modelled as 20 uniform and 4 mixed:

```text
                 before      after
time            830.1 us   221.8 us    3.7x
allocation      1,185 KB     233 KB    5.1x less
```

The mix is a model rather than a measurement, so the real gain depends on how much of a real chunk is uniform. The canonical bytes are unchanged by construction, and were checked against the shipped algorithm on uniform sections of five different states, on a section made uniform by writing rather than by its palette, and on states that differ only by a block property.

A game test on the same machine afterwards reported:

```text
177 ticks, mean 195.7 us, max 7570.6 us (15.141% of a 50 ms tick)
```

The fingerprint from that run is byte-identical to the committed reference, so nothing about what was captured changed.

The mean fell by 5.5 times, more than the 3.7 measured outside the game, because a real chunk is more uniform than the model.

**The maximum did not fall with the mean.** It halved where the mean fell by more than five, so most of that tick was never the per-block work: less garbage makes a young-generation collection rarer without making one shorter.

Reporting where the worst tick fell settled what it was. A later run:

```text
191 ticks, mean 373.0 us, max 7895.8 us (15.792% of a 50 ms tick); worst was tick 1 of 191, 4 tick(s) over 5 ms
```

The worst tick is the **first** one, which is a cost paid once as a session warms up rather than a stutter that recurs, and four of 191 ticks exceeded 5 ms. At 7.9 ms against a 16.7 ms frame none of them is a dropped frame at 60 fps. A maximum alone could not have told these apart from a session stuttering every few seconds, which is why the position and the slow-tick count are reported with it.

This is one machine, one scripted world, and a small pinned area. A player exploring loads far more chunks, so this is a floor for how often the cost is paid, though not for how large any single payment is.

The Go core now has benchmarks over the committed fixtures and the committed capture bundle, covering the two halves [`test-strategy.md`](test-strategy.md) asks for. Measured on one Windows machine, so they are indicative rather than guarantees:

```text
decode a block section              ~12 us          8-17 allocations
decode a high-palette section      ~592 us       12,293 allocations
encode a block section         ~354-448 us        4,121 allocations
encode a full-height chunk        ~10.6 ms       98,823 allocations
import one bundle, fresh archive ~32-53 ms        1,742 allocations
import one bundle, already held  ~15-20 ms        1,700 allocations
```

The import figures above use the committed capture bundle, which carries four components. A bundle from a real session carries about fifty.

Importing 158 of those measured 2 minutes 25 seconds, or roughly 918 ms each. Almost none of that was the durability it looked like.

Every component's path was resolved from the volume root, opening a handle per element, and every component in a bundle shares nearly all of that walk: fifty components spent 98 ms repeating it against 8 ms of actually opening the files. Resolving each directory once per bundle brought the same 158 bundles to 36 to 45 seconds.

What was left did look like durability, and the object store did fsync once per component. But it fsynced before checking whether the object was already there, so it was making an object durable and then deleting it. That session held 7,900 components and 52 distinct objects: 99% of those fsyncs were for bytes already on disk. Checking first brought the same import to 23 to 25 seconds, and a second import of the same session to the same figure rather than to the 38 seconds it cost before.

The remaining floor is genuine. An object the archive has never seen must be written and forced to disk before the import is acknowledged, and no amount of ordering avoids that. What the archive no longer pays is the same cost for bytes it already has.

Two of these are worth reading carefully. Encoding costs roughly thirty times what decoding does and allocates about once per block state; the reference encoder is used by the fixture tooling rather than on any path a player waits on, so this is a known cost rather than a problem to date. Import spends its time in durability, not computation: seventeen hundred allocations against tens of milliseconds is the signature of the fsync calls that put an observation on disk before the import is acknowledged. Reimporting an observation already held is cheaper but not free, because it still reads and hashes every component rather than trusting the digest the bundle declares.

## Not verified

**How a converted world renders.** A 1.21.11 server has opened one and answered for its contents, which is recorded above. No 1.21.11 *client* has opened one, so nothing here covers lighting, or how the seams between converted and generated chunks look to a player.

**Conversion of a world that actually loses something.** The world that has been through a real older release loses nothing, because it uses none of the thirty blocks 1.21.11 lacks. What each policy does when there is something to lose is covered by tests against the 1.21.11 profile — the block is named in the report under every policy, the default policy leaves the chunk unwritten rather than substituting for it, the report policy refuses outright, and a chunk of blocks the release does have passes through unchanged — but no such world has been opened in Minecraft.

**A released build installing, from the release itself.** The `worldledger-desktop-windows-amd64.exe` published with v0.3.0 was downloaded, its SHA-256 matched the checksum shipped beside it, and it reports version 0.3.0 and carries the address of that release's own mod jar. Installing with it put the released jar into the mods folder with a digest matching the published asset, and the health check then read six green lines.

Uninstalling with it, after the launcher settings had been rewritten the way opening the launcher rewrites them, left nothing behind and kept everything that was not ours. A checksum of every file two levels down under `.minecraft` came back to what it was before, apart from the launcher timestamp that had been changed on purpose.

The address the application fetches from was then requested with no credentials at all, which is what a stranger's copy does: 200, 128,149 bytes, and a digest matching what `SHA256SUMS.txt` on the releases page declares.

**An unsigned application on a stranger's Windows.** SmartScreen will warn about a binary from an unknown publisher, and security software may quarantine it. Nothing here is signed, the README says so, and whether to buy a signing certificate is a decision nobody has made.

**The exported world opened in Minecraft.** The chunks are in the world and an independent reader agrees about what they contain. Nobody has yet loaded that world and walked around in it, which is what the 26.2 export was verified by once before and this particular world has not been.

**The desktop application anywhere but Windows.** It compiles for Linux and macOS and the browser path is the whole application there rather than a degraded one, but no window is created on either and neither has been run.

**Race detection on Windows.** The race gate needs cgo and is run in Linux CI only.

**Running the client game test without Gradle.** Gradle can fail to start, with `Unable to establish loopback connection` from every task before any build logic runs. The client game test is the only end-to-end exercise of capture and the only thing that re-checks the capture fingerprint, so losing it to that would mean losing the gate.

`scripts/run-client-gametest.ps1` in the repository runs it anyway. Loom writes the whole launch specification to `adapters/fabric/.gradle/loom-cache/launch.cfg` during a normal build, and it stays valid afterwards, so the script compiles the four source sets with `javac` into the directories that file names, prepares the run directory exactly as `prepareClientGametest` does, and launches the same client through dev-launch-injector.

It does not accept Mojang's EULA. `build.gradle` deliberately refuses to make that decision for an operator, and the script refuses in the same way: the run directory has to carry an `eula.txt` from a run someone authorised. `-BuildOnly` compiles and prepares without opening a window.

**The Gradle failure and the game test failure have one cause, and it is a directory.** The game test starts a Minecraft dedicated server; its networking is Netty; a Netty event loop is a `java.nio` Selector. On Windows a Selector's wakeup pipe is a pair of AF_UNIX sockets — `WEPollSelectorImpl` constructs a `PipeImpl` with `preferAfUnix` set — and the socket file is created in `%TEMP%`. Where an AF_UNIX `connect()` to that directory is refused, the JDK reports `Unable to establish loopback connection`, naming a mechanism it did not use. That is why TCP loopback working proves nothing, why `Pipe.open()` still works (it asks for TCP), and why Gradle prints the same words from the same cause: its daemon connector opens a Selector too. Minecraft reaches it several minutes into startup and calls it `failed to create a child event loop`.

Where this was diagnosed, AF_UNIX `connect()` failed with `WSAEINVAL` for every path under `%USERPROFILE%\AppData` — which is where `%TEMP%` lives — while `C:\Windows\Temp` and the checkout itself both worked. `jdk.net.unixdomain.tmpdir` decides where the socket file goes. Setting it in the JDK's own `conf/net.properties` fixes every JVM started from that JDK at once, including the Gradle daemon, which nothing in this repository launches. With that set, Gradle runs normally and this script stops being the only way in.

The script no longer stops at the check. Its preflight opens a Selector the way the JDK would, and only if that fails looks for a directory where one can be opened and passes it to the client, so a machine that has not had `net.properties` set still runs the game test. Everything before the launch — compiling all four source sets, expanding the manifest, preparing the run directory — works regardless, which is what `-BuildOnly` is for.

This is a fallback, not a replacement. Gradle remains what CI uses and what produces a release JAR.

## Known gaps that are not defects

- `level.dat` is never generated. Exports are written into a world the target client created.
- Block entity payloads are not migrated across releases, and are dropped by default when converting.
- Release profiles carry no block state property definitions. A client jar describes block states only through its rendering definitions, which omit properties that do not change a model, so property-level validation would reject valid states.
- Structure placement is modelled; biome and terrain generation are not.
