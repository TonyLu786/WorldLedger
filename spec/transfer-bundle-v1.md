# worldledger.transfer-bundle/v1

A transfer bundle moves observations and objects between two archives without a
service between them. It is an ordinary directory: copy it, mail it, mirror it,
or serve it as static files.

The receiver verifies everything it takes in. Nothing in this format asks the
receiver to trust where the bundle came from.

## Layout

```text
<bundle>/
  bundle.json
  observations/<observation-id>.json
  objects/sha256/<digest[0:2]>/<digest[2:4]>/<digest>
```

## bundle.json

```json
{
  "schema": "worldledger.transfer-bundle/v1",
  "created_at": "2026-08-14T00:00:00Z",
  "observations": ["<observation-id>", "..."],
  "objects": [{"digest": "<sha256>", "size": 8262}]
}
```

Unknown fields are rejected. `observations` and `objects` list exactly what the
bundle carries; a declared entry that is absent is an error rather than a
warning.

## What the receiver checks

- Every object is stored through the verifying path. Bytes that do not hash to
  the declared digest are refused, not written.
- Every observation must validate against the archive's stored-record rules,
  which recompute its identity. Renaming a contributor, moving a record to
  another chunk, or changing its instant all change the id it must hash to, so
  none of them can be smuggled through.
- Every component an observation references must resolve in the receiving
  archive after the objects have been stored.
- A record the archive already holds is skipped. Importing the same bundle twice
  changes nothing.

A bundle from an untrusted peer therefore cannot introduce anything the archive
would not have accepted from its own adapter.

## What is negotiated, and why the two halves differ

Objects are chosen by comparing fingerprints. A fingerprint carries state and
component digests only, so it says exactly which objects a peer lacks. This is
the same property that makes deduplication work, used in the other direction.

Observation records are chosen by comparing manifests, when the sender has the
peer's. A manifest digests observation identities per chunk, so a mismatch says
the two sides disagree about a chunk without saying which record is missing;
the sender includes every record for those chunks, which is the smallest safe
answer to that question.

Without the peer's manifest, every record is included. That is correct but can
be wasteful: on an archive where deduplication was extreme, 158 records
outweighed the 8 KiB of objects actually missing. Sending only the records that
reference a missing object would be smaller still and wrong, because it leaves
two mirrors agreeing on every byte while disagreeing about who observed what.

## Convergence

One bundle moves data one way. Two archives converge when each has sent to the
other, at which point their manifest roots are equal.
