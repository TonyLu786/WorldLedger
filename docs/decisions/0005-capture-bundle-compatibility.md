# ADR 0005: What a capture bundle does when the two halves are different ages

**Status:** accepted

This records a rule that already governed the code and was not written down,
and one correction to [ADR 0004](0004-archive-layout-changes.md), which
described the failure wrongly.

## Context

The mod and the archive core are obtained separately. A player updates one and
not the other, or updates both and one arrives first. Holding two halves of
different ages is the ordinary state, not the exceptional one, and it is nobody's
mistake.

ADR 0004 said a version mismatch there "fails as capture that silently stops
importing" and cited it as more urgent than the archive layout question it was
actually about. That was written without checking and it is false. Both front
ends report it: the terminal lists every bundle that would not import and exits
non-zero, and the window puts them on the import screen.

What was true is smaller. The report was loud and unreadable. Rejecting the
manifest's unknown fields happened before anything read the schema, so a bundle
from a later adapter came back as `json: unknown field "capture_note"`. True, about the wrong
subject, and handed to somebody whose situation is that two programs need to be
the same age.

## Decision

**A capture bundle is strict about its fields, and a version mismatch is
explained as one.**

Two parts:

- **Unknown fields stay rejected.** An adapter that adds a field declares a new
  schema version. This is what the code has always done and what
  `spec/transfer-bundle-v1.md` has always said about its own manifest;
  `spec/capture-bundle-v1.md` did not say it and read as an invitation to do
  otherwise. It says it now.
- **The schema is read before anything is validated against it.** A mismatch
  names the two versions, says which half is ahead, says which one to update,
  and says the capture is still where it was. Which half is behind is knowable
  at that point and nowhere else, and it is the whole of the answer.

The bundle is never moved, altered or removed on a version mismatch. A capture
that cannot be imported today is the only copy of what somebody saw, and it
imports unchanged once the halves match.

## Why not tolerate additive fields

The obvious alternative is to ignore unknown fields, or to put them under a
reserved key that older importers skip, so that a newer adapter keeps working
with an older core. It is how a great many formats evolve and it was rejected
here for one reason.

A capture bundle is the input to an identity. An observation id is derived from
the manifest's fields, and an importer that ignores a field it does not
understand derives an identity from part of a record while believing it has the
whole of it. Two builds would then disagree about what the same capture is
called, which is the one disagreement this project cannot recover from: a
fingerprint comparison reports a divergence that is not there, and a redaction
fails to find the record it names.

Tolerance is affordable for a field that provably cannot reach an identity. That
is a narrower rule than "additive fields are fine" and it needs the format to
say which fields those are, which v1 does not. It is available to v2 and is not
available by relaxing v1.

## Consequences

- A new adapter field is a schema bump, which is a visible act rather than a
  quiet one.
- Somebody whose halves are different ages is told which one to update, in those
  words. Verified against the built binary: a bundle declaring `v2` is refused
  with the two version numbers, the direction, and what to do.
- The strict decode still runs, and a field arriving under a version this build
  does know is now its own complaint, because that is a bug in whatever wrote
  the bundle rather than a version anybody can update past.
- `spec/capture-bundle-v1.md` gained the three rules it enforced and did not
  state: unknown fields, duplicate keys, and the nesting cap.
