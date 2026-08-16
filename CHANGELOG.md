# Changelog

## v0.2.0

v0.1.0 proved the thing works: a live multiplayer session becomes an archive,
and the archive becomes a world you can walk around in. What it did not do was
make that pleasant, or fast, or possible to check against somebody else's copy.

This release is mostly about the second of those. Importing a session went from
two and a half minutes to under half; capture asks the client thread for about a
fifth of what it did; and the path from an empty directory to a world now names
its own next step at every stage instead of leaving you to find it. Walking that
path from the beginning turned up defects rather than only rough edges, and two
commands could not have worked as documented.

It also adds the two documents that let people compare what they hold. `epoch`
says what an archive believes a moment looked like, with a root digest, so two
contributors can find out whether they would export the same world without
sending each other one. `diff` says what changed between two moments, and keeps
apart what changed from what nobody went back to look at.

### `landmark` gives places names, so coverage can use them

A chunk coordinate is not how anybody thinks about where they have been.
`coverage` reported "157 chunks, x -3..3, z -3..3" where a person would say
"spawn", and an archive full of real exploration read like a spreadsheet.

`worldledger landmark set` names an area, and `coverage` then reports each one:
`spawn  all 25 chunk(s)`, `far east  none of its 121 chunk(s)`. Pass
`--landmark` to scope a report to one.

A landmark is a declaration, not an observation, which is why it lives beside
publication policies and redactions rather than on a chunk. An observation is
evidence that a client saw certain bytes at an instant; a landmark is somebody
asserting that an area means something, and letting the second be mistaken for
the first would put an opinion into the record of what was seen. Every landmark
records who declared it, an unattributed one is refused, and removing one
touches nothing observed.

Landmarks stay local. A transfer bundle carries observations, and a name
somebody chose for a place is not one.

Identity is the server, dimension and folded name, so declaring "Spawn" over
"spawn" moves the existing landmark rather than leaving two with one name. The
bounds deliberately do not reach the identity: moving a landmark is editing it.

A chunk-coordinate box is now `model.ChunkBounds`, shared with redaction, which
names an area for the opposite reason. It replaced a second copy of the type and
a second parser, and is named to avoid the Anvil region file that `Region`
already means here.

### `epoch` records what an archive says a moment looked like

An exported world carries no record of where it came from. Two people can
export the same server at the same instant, from archives holding different
observations, get different worlds, and have no way to find that out short of
comparing region files.

`worldledger epoch` writes the document that answers it: every chunk position,
the state chosen there, and a root digest over the two. `--compare` puts two of
them side by side and exits non-zero when the worlds differ, so it can gate a
publication step.

What the root covers is the whole design, and it covers positions and states
and nothing else:

- **Not contributors.** Two archives that selected the same state through
  different people export the same world. Digesting who provided it would report
  agreement as disagreement.
- **Not confidence.** An archive holding two agreeing observations calls a chunk
  corroborated where one holding a single observation calls it single-source,
  and both export the same blocks.
- **Not when the manifest was written**, which is not a fact about the world.

Confidence differences are reported separately, because they are worth knowing
and are not what makes a world. On two real archives differing by a second
contributor, the roots matched and forty chunks were listed as corroborated on
one side and single-source on the other.

A manifest whose recorded root disagrees with its own entries is refused rather
than recomputed, because comparing against an edited file compares against
something that never existed.

### The mod tells you what it is doing

Everything the adapter did was reported in one chat line on join and then never
again, which is no use to someone who wants to know now. There are now client
commands, handled locally, working on any server, needing no permission:

- **`/worldledger`** or **`/worldledger status`** — whether capture is on and
  under whose name, what this session has taken, how many bundles are waiting,
  and where they are. When capture is off it names the file that turns it on.
- **`/worldledger spool`** — the spool path and the command that imports it.
- **`/worldledger reload`** — re-read `capture.properties` without restarting
  the client, which used to be required for a one-word edit. `coalesce_ticks`
  and `queue_capacity` are consumed when capture starts and are the two a
  reload cannot change, so the notice names them instead of implying otherwise.

A finished session now says where its chunks went. The spool path previously
appeared only when the spool was full, so the ordinary outcome of a good session
was a chunk count and no way to find the chunks.

What a player is shown lives in `CaptureStatus` and `CaptureNotices`, which
carry no Minecraft types and are tested without a client.

### Capture asks the client thread for far less

A section where every block is the same is stored by Minecraft with a
single-value palette, and the client can recognise that in constant time.
Capture was reading all 4,096 positions anyway and being told the same thing
4,096 times. Most of a chunk is like this: everything above the terrain is air,
and a chunk carries 24 sections.

Measured outside the game on real containers, over a chunk of 24 sections
modelled as 20 uniform and 4 mixed: 830 us to 222 us, and 1,185 KB of
allocation to 233 KB. The mix is a model, so the real gain depends on how much
of a real chunk is uniform.

The captured bytes do not change. The encoder's output depends only on the
section index and the 4,096 values in order, and it is handed the same values
either way; this was checked against the previous algorithm on uniform sections
of five different states, on a section made uniform by writing rather than by
its palette, and on states differing only by a block property, which is where a
wrongly keyed cache would otherwise pass unnoticed. `hasOnlyAir` would have been
the obvious test and is the wrong one: `cave_air` and `void_air` report as air
and canonicalize to different strings.

A game test afterwards reported `177 ticks, mean 195.7 us, max 7570.6 us`
against `169 ticks, mean 1080.1 us, max 15216.3 us` before, and produced a
capture fingerprint byte-identical to the committed reference.

The mean fell by 5.5 times. The worst tick only halved, so most of it was never
the per-block work: less garbage makes a collection rarer without making one
shorter. Reporting where that tick fell settled what it is. A later run gave
`191 ticks, mean 373.0 us, max 7895.8 us; worst was tick 1 of 191, 4 tick(s)
over 5 ms`: the worst tick is the first one, which is a session warming up
rather than a stutter, and none of the four slow ticks is a dropped frame at
60 fps.

### The command line answers the questions people actually ask

Walking the path from an empty directory to a world turned up defects rather
than only rough edges.

- **Two commands could not work as documented.** `inspect` and `verify`
  defaulted `--dimension` to `overworld`, and an archive stores the namespaced
  `minecraft:overworld` a client reports, so accepting the default was
  guaranteed to match nothing; `inspect` then printed a bare `null`. Every
  command now defaults through one constant, checked by a test that reads the
  sources.
- **`coverage` reported a mistyped server name as `chunks 0` and exited
  successfully**, which reads as an answer about the world rather than about the
  arguments. It now names the servers the archive holds, as `export` and `diff`
  already did.
- **`--help` and `-h` returned an error on twenty of twenty-one commands.**
  Usage now lives in one place, is what both a missing flag and `--help` report,
  and goes to stdout with a zero exit. A test reads the dispatch table, so a
  command added later cannot arrive without either. An unknown command suggests
  the nearest one, and says nothing rather than guessing when nothing is close.
- **Opening something that is not an archive** named `VERSION`, an internal
  file. It now names the directory and the `init` line.
- **The last step was the worst.** Following every instruction correctly ended
  at `does not look like a Minecraft world: CreateFile ...level.dat`. Export
  writes into a world rather than creating one because a seed and generator were
  never observed, and inventing them is the thing this project refuses to do.
  That reason, and the three steps that get past it, are now in the message.

The path also narrates itself. `init` names the import command; `ingest-spool`
finds the spool under the usual Minecraft directory for the platform, prints
which one it chose, and can still be pointed elsewhere; it and `policy set` end
by naming what comes next with real paths filled in. The publication decision is
not automated away — it is a decision someone has to make — it is only announced
before it blocks anything.

### `diff` says what changed between two moments, and what it cannot say

`coverage` reports the world at one moment. The new `diff` command reports the
interval between two, and keeps apart two things that look identical in any
comparison of exported worlds:

- **`unchanged`** means somebody observed the chunk again during the interval
  and found the same state. That is a claim about the world.
- **`not revisited`** means the state is the same only because it was carried
  forward from before the interval began. Nobody looked. That is a claim about
  the archive, and reporting it as unchanged would let an archive that stopped
  being updated read as a world that stopped changing.

Chunks first observed during the interval, and chunks observed only after it,
are counted separately again, so the summary never implies a comparison it did
not make. The output states plainly how many of the chunks it can speak for.

Each changed chunk is listed with the contributors who observed the new state
and when, most recent first, and a chunk whose contributors disagree about the
resulting state is marked rather than quietly resolved. `--json` carries the
whole comparison, including every observation made inside the interval.

Intervals come from `--from`/`--to` or from `--since 24h`. A comparison that
could not compare anything reports the range that was actually observed and the
command that would work, rather than printing zeroes.

Withheld observations are filtered out on the way in, on the same path `export`
and `coverage` already use.

### Importing a session is about six times faster

A 158-bundle session took 2 minutes 25 seconds to import. It now takes 23 to 25
seconds, and importing the same session a second time costs the same rather than
most of the original.

Neither of the two costs was where it appeared to be. Both were found by
measuring before changing anything, and the benchmarks that found them are
committed alongside the fixes.

- **Each bundle directory is resolved once instead of once per component.** The
  check that keeps a component from escaping its bundle resolved every path from
  the volume root, opening a handle per element, and fifty components in one
  bundle repeat nearly all of that walk. It cost 98 ms per bundle against 8 ms of
  actually opening the files. The check itself is unchanged: the final element is
  already proven not to be a symlink, so resolving it is resolving its parent
  with the name appended.
- **A component the archive already holds is no longer written again.** The
  object store wrote each component to a temporary file and forced it to disk
  before checking whether that object was already there, then deleted what it had
  just made durable. In the measured session 7,848 of 7,900 components were
  already stored. The incoming bytes are still read and hashed and still have to
  match the digest the bundle declares, so a bundle whose component file
  disagrees with its own manifest is rejected exactly as before, including when
  some other bundle already contributed the real object.

## v0.1.0

The verification the earlier builds were waiting on has been done. Both
pre-releases said so plainly; this one says it is finished.

### The thing that was outstanding

The same observed world state, captured by a Windows client and by a Linux
client in CI, canonicalizes to identical bytes. The two fingerprints agree on
all 157 chunks both captures observed, and the two files are byte-identical.
The game test pins the world seed, generator and view distance, so a difference
between the two could only have come from the encoder.

That reference is now committed. CI compares every Linux capture against it and
fails on a disagreement rather than reporting one after the fact.

### Also in this release

- **A whole spool imports in one command.** `ingest-spool` takes in every ready
  bundle and clears them once the archive has them, because a spool nothing
  empties grows until the disk does.
- **The spool stops growing without a limit.** It refuses to write past its
  budget and never deletes what it already holds to make room, and identical
  component bytes are stored once instead of once per bundle.
- **`status` says what an archive holds and what to do next**, including which
  servers still have no publication decision.
- **Errors say what to do.** An empty selection used to give one timestamp
  whether the archive was empty, the server name wrong, the dimension wrong, or
  the moment too early. Those read differently now.
- **The adapter reports itself in game.** Whether capture is on, under whose
  name, and what the previous session captured including anything dropped.

### Measured, and not comfortable

Capture costs the client thread a mean of 1.08 ms on the ticks that do work,
and 15.2 ms on the worst one. A frame at 60 fps has 16.7 ms. The worst case is
one full-height chunk and it is bounded, but it can cost a frame, and
`docs/status.md` says so rather than reporting the mean and stopping.

### Still not verified

No converted world has been opened in an older release, and there is no
committed profile for any release other than 26.2, because building one
requires that release's own artifact.

## v0.1.0-alpha.2

Fixes what the first build got wrong about presenting itself. No change to how
anything is captured, stored, or reconstructed.

### Fixed

- The first line of `worldledger --help` was corrupted. A source file had been
  edited through a tool that decoded UTF-8 as a legacy codepage and wrote the
  result back, turning an em dash into a CJK character followed by a question
  mark. It parsed, every test passed, and the damaged bytes reached the
  published binary.
- Terminal output is now ASCII throughout, including the em dashes that were
  valid UTF-8. What a program prints should not depend on the reader's console
  codepage, which on Windows is frequently not UTF-8.
- The archive you download now carries every document its README links to.
  Previously nine of those links pointed at files that were not in it.
- Only the mod JAR is published. The sources JAR sat beside it with a similar
  name, and installing the wrong one fails silently.
- The help text pointed at a source tree that someone who downloaded a binary
  does not have.

### Guards added

Each of the above could recur without anything noticing, so each has a check
that fails rather than a fix that hopes.

- Go sources must be ASCII, and no tracked text file may carry the residue of an
  encoding round trip. Both were verified by reintroducing the exact corrupted
  bytes and confirming the tests fail.
- The release build asks the binary what version it reports and refuses a
  mismatch, after an earlier attempt at version injection silently applied to
  the mod and not to the CLI.
- Packaging verifies that every relative link in the packaged documents resolves
  inside the package.
- Publishing verifies that the changelog's top entry is for the tag being built.

## v0.1.0-alpha.1

First published build. A pre-release, because one verification the project
considers necessary has not been done: see the end of this file.

### What it does

Capture a live Minecraft 26.2 multiplayer session from an unmodified client,
store what was observed as immutable content-addressed evidence, read the
archive back at a chosen moment, and write it into a world the game can open.

- **Archive.** Content-addressed objects, immutable observations, per-chunk
  history, integrity check, portable manifests.
- **Capture.** Client-only Fabric adapter for Minecraft 26.2. Crash-safe spool;
  nothing observed is discarded to keep collecting.
- **Reconstruction.** Point-in-time selection that distinguishes corroboration,
  supersession, and conflict, then writes Anvil region files. Chunks nobody
  observed are left unwritten rather than filled in.
- **Cross-release conversion.** A separate command, into a separate world, with
  declared and reported loss.
- **Publication policy.** Every server needs one explicit, attributed decision
  before the archive will build a world from it.
- **Redaction.** Contributor and region scopes, withheld from anything built for
  sharing. Purging reports what it could not remove and who still needs it.
- **Attestation.** ed25519 signatures over observation identities, with an
  explicit registry of recognised keys.
- **Exchange.** Two archives converge over a plain directory, with no service in
  between and every byte verified on arrival.

### Verified

Reconstruction has been checked in an unmodified 26.2 client: an exported chunk
loads and renders correctly, including negative sections and block state
properties, confirmed in game and by an independently written Anvil reader.

Capture has been exercised against a real 26.2 client, and the client game test
runs headless in Linux CI on every push. Canonical bytes have committed golden
vectors that the Go and Java implementations both reproduce exactly.

`docs/status.md` lists every claim with the evidence behind it.

### Not verified

The same world state captured on two different platforms has never been compared
digest for digest. The tooling for it exists and Linux CI publishes its half on
every push, but until a Windows capture is compared against it, the canonical
encoding is platform independent by construction rather than by measurement.

That is why this is `alpha.1` rather than `0.1.0`.

Nothing has measured the adapter's cost on the client frame budget, and no
converted world has been opened in an older release.
