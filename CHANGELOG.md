# Changelog

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
