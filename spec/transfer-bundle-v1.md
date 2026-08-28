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
  "objects": [{"digest": "<sha256>", "size": 8262}],
  "attestations": ["<observation-id>.0.json", "..."]
}
```

`attestations` is optional and absent when nothing carried a signature, so a
bundle written before signatures travelled is still a valid bundle.

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
- Every attestation must verify against the observation id it names, and must
  name a record this bundle carries. One that does neither is refused, and the
  whole receive fails rather than storing the rest.

A bundle from an untrusted peer therefore cannot introduce anything the archive
would not have accepted from its own adapter.

## What identity does not settle, and what the signatures are for

Recomputing an id proves a record was not altered. It does not prove who wrote
it: an id is a hash of the record, so a record invented from nothing hashes
correctly to its own contents, contributor label included. Anybody can mint one
naming anybody.

That is what the attestations are for, and for a while they did not travel. A
bundle carried `observations/` and `objects/` and nothing else, so an honestly
transferred record and a fabricated one both arrived unsigned and read
identically — the exchange was the one place where attribution stopped meaning
anything, which is the opposite of what it is for. Signatures now travel in
`attestations/`, listed in the manifest, verified on arrival against the record
they name.

This still does not make a contributor label true. It makes an unsigned record
distinguishable from a signed one, and leaves the question of whose keys are
recognised where it belongs: a local, attributed decision in the identity
registry, which reports a valid signature from an unregistered key as exactly
that rather than as an endorsement.

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
