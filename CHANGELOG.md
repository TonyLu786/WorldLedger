# Changelog

## Unreleased

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
