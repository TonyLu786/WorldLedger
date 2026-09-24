# ADR 0004: What a change to the archive layout is allowed to cost

**Status:** proposed

## Context

An archive is the thing somebody is keeping. It outlives every build that
touches it, which is the whole proposition, and it is the one artefact this
project cannot ask anybody to recreate.

`VERSION` holds a layout number and `Open` refuses anything that is not this
build's. There is no migration, in either direction, and none has ever been
written. That was fine while the number had only ever been `1`. It stops being
fine at the moment there is a `2`, because that is also the moment it is too
late to design: archives written by `1` are already on disks, and whatever
`Open` does about them is decided by code that shipped before the question was
asked.

The refusal is now informative — it says which layout was found, which this
build reads, and whether the archive came from a later WorldLedger or an earlier
one — so somebody meeting this is told which way to go. That is the smallest
useful thing and it is not a migration.

## What is actually at risk

Not as much as the absence of migration code suggests, and the reason is a
property the archive already has.

The archive holds two kinds of thing:

- **Records.** `observations/` and `objects/`. An observation is an immutable,
  content-addressed claim; an object is bytes addressed by their own digest.
  Neither can be derived from anything else, and losing one loses evidence.
- **Arrangement.** `index/`. Every line of it is an observation id filed under
  the chunk that observation names, and both of those come out of the
  observation file. It decides nothing and records nothing.

`Archive.RebuildIndex` now derives the whole of the second from the first, and
is reachable as `worldledger fsck --rebuild-index`. It was written for a
different reason — an index entry naming a missing observation, and an
observation missing from its index, are the two things the integrity check
reports most often and neither had a fix — but it settles this question as a
side effect. It was checked by deleting `index/chunks` entirely and rebuilding:
the archive passes its own check again and its fingerprint is unchanged.

So the cost of a layout change depends on which kind it touches, and those two
costs are not close to each other.

## Decision

**State in the format specification which parts of an archive are records and
which are arrangement, and hold layout changes to the second wherever a change
can be made there.**

That makes three things true at once:

- A layout change that only rearranges is migrated by deriving again. No
  journal, no half-migrated state, no one-way step, and it is already possible.
- A layout change that touches records is a different and much larger act, and
  it is visible as one when it is proposed rather than discovered when it lands.
- `fsck --rebuild-index` becomes the migration path rather than a repair tool
  that happens to also work, so it is exercised by ordinary use.

**And read more layouts than are written.** A build reads `N` and `N-1` and
writes `N`. An archive upgrades the first time something rearranges it, which
for a rearrangement-only change is the rebuild above. Nothing has to be migrated
before it can be opened, and an archive that is only ever read is never touched.

## What this does not decide

Whether the capture bundle between the mod and the core gets the same treatment.
It has the same shape of problem and a worse exposure: the mod and the core are
downloaded separately and a player can hold an old one of either, and
`spec/capture-bundle-v1.md` has the importer rejecting unknown manifest fields
outright. A layout change on that side does not fail at `Open` with a sentence
about which way to go; it fails as capture that silently stops importing. That
is a second ADR and probably a more urgent one.

## Consequences

- The specification gains a paragraph that is a promise: the records are
  `observations/` and `objects/`, and everything else is derived. Anything
  proposing to change that has to change this ADR first.
- `Open` gains a set of readable layouts rather than one, when there is a second
  one to read.
- The rebuild is on the critical path of a layout change, so it needs to stay
  correct for reasons beyond repair. It has tests for a destroyed index, a stale
  entry, and a record it cannot place.
- Nothing about today's archives changes. There is one layout and this describes
  what happens when there are two.

## Alternatives considered

**In-place upgrade with a journal.** The purge journal already shows the shape:
write down what is about to happen, do it, replay on the next open if it was
interrupted. It is proven here and it is one-way, it needs new crash-safety code
for every migration rather than once, and it makes an archive unreadable by the
build that wrote it. Warranted for a records change and disproportionate for an
arrangement one.

**Convert on first open.** Rejected because it makes opening an archive a write,
and a great deal of this program opens an archive to answer a question.

**Refuse and require an export-import round trip.** Honest and expensive: it
asks somebody to have twice the disk free, and the transfer path filters
redactions, so a round trip is not the identity function on an archive.
