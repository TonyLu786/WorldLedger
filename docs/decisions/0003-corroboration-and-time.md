# ADR 0003: Whether corroboration may be counted across time

**Status:** proposed

## Context

`epoch.SelectChunkWithin` decides, for one chunk and one moment, which observed state a reconstruction should use and how much confidence to attach to it. `export`, `convert`, `coverage` and the desktop application all act on that answer, and `StatusCorroborated` is the strongest thing this project says about any piece of data.

The package already knows that time changes what disagreement means. `StatusSuperseded` exists precisely because a Minecraft world is mutable, and its comment says so: two different states minutes apart are the expected case rather than a contradiction, and reporting them as conflicts would bury the few that are real.

That reasoning is applied in the wrong place. The selection runs in this order:

1. `latestPerContributor` reduces the observations to one per contributor: their most recent at or before the epoch. **There is no lower bound on how old that may be.**
2. States are grouped and sorted by how many contributors reported each.
3. If the leading group has at least two contributors and strictly more than the runner-up, the answer is `corroborated` and the function returns. **No time is consulted.**
4. Only if step 3 does not fire does `spread` measure the observations and decide between `conflict` and `superseded`.

So the "the world simply changed" branch is unreachable whenever the older side has the numbers.

### What that produces

Six contributors, one chunk. Four of them last looked between twenty and sixty minutes ago and saw the state that was there then. Two of them looked one minute ago and saw something different, because somebody built.

```
status=corroborated  contributors=[ann bob cat dan]  observedAt=12:40  rejectedGroups=1
```

The chunk exports as the pre-build state, labelled with this project's strongest confidence word, at a moment when the two most recent observations of it both say otherwise. Nothing is reported as superseded and nothing is reported as conflicting.

A smaller version of the same thing: four contributors disagreeing two-against-two within three seconds is correctly `conflict`, and adding one twenty-minute-old observation agreeing with either side turns it into `corroborated`.

Both were reproduced against the current implementation.

### There are two algorithms, and the documented one is not the one that exports

`docs/architecture.md` describes verification as grouping observations for a chunk into a configurable time window and labelling what is inside it, and states that a conflict is never resolved by majority vote in the core, because time uncertainty, world changes, packet ordering and capture bugs need more context than a vote count provides.

That is an accurate description of `internal/verify`, which does window first and does not vote. It is not a description of `internal/epoch`, which is the one `export`, `convert` and the desktop application use to decide what a world contains. There the vote comes first, across unbounded time, and the window is only consulted when the vote fails to produce a winner.

So the reasoning this ADR proposes is not new to the project. It is already written down as the project's position; one of the two selectors implements it and the other does not.

### Why it matters more here than it would elsewhere

An archive that stores what a world looked like at a moment is only worth having if the moment is real. Time travel and honest unknowns are the two things this project claims a world downloader structurally cannot do. A confidence label that counts a vote from an hour ago as equal to one from a minute ago is not a small inaccuracy in that claim; it is the claim being least true exactly where somebody would rely on it.

It is also not a Sybil problem, and no amount of contributor verification fixes it. Every contributor above is a distinct, honest person who reported what they actually saw.

## The distinction the algorithm is missing

Agreement and currency are two different questions, and corroboration currently answers the first while being read as an answer to both.

- **Agreement** is about whether independent people saw the same thing. Two contributors who visited a chunk a week apart and saw the same state do corroborate each other, and that is worth keeping. Nothing changed in between; two people confirm it.
- **Currency** is about whether that state is still what the chunk holds at the epoch. An observation only speaks to that until somebody observes otherwise.

The rule that follows: **a state may not be reported as the state at a moment on the strength of votes that predate a contradicting observation.** Older agreement still corroborates; older disagreement is superseded, not outvoted.

## Options

### A. Leave it, and document it

Cost: nothing. The label keeps a meaning most readers will not guess, in the one place they are most likely to trust it. The trust model would have to say that `corroborated` counts agreement without regard to age, which is a hard sentence to write next to "honest unknowns".

### B. Bound the vote by recency

Take the most recent eligible observation. Partition the rest into those within the simultaneity window of it and those outside.

- If the contemporary set holds more than one state, that is a `conflict`, decided among the contemporary observations alone.
- Otherwise the contemporary state is the state at the epoch. Older observations that agree with it add to its corroboration count. Older observations that disagree become `superseded` evidence and do not vote.

This fixes both reproductions. The four stale contributors above stop outvoting the two recent ones, the chunk reports `superseded` with the built state selected, and the four earlier observations remain in `Rejected` where they can be read. The three-second disagreement stays a `conflict`, because a twenty-minute-old observation is no longer contemporary and no longer votes.

Cost: fewer chunks report `corroborated`, and the ones that stop are exactly the ones where the corroboration was carried by observations somebody else had already contradicted. Selections change, so any archive re-exported after this may differ, and `docs/status.md` figures derived from a snapshot summary would need re-measuring.

### C. Report the ages and leave the decision to the caller

Add the observation-time span of the winning votes to `Selection`, and let each caller decide what to do with it.

Cost: every caller has to make the judgement, and none of them are placed to. `export` writes a region file; it has nowhere to put "corroborated, but by observations up to an hour old". It moves the problem rather than solving it, though it is a reasonable addition alongside B.

### D. A separate status

Keep the vote as it is and add `corroborated-stale` for a leading group whose votes predate a contradicting observation.

Cost: `Status` is currently a small closed set that maps cleanly onto colours in the desktop application and onto counts in the summary. A sixth value is not free. It also still selects the stale state, so the export is unchanged; only the label improves.

## Recommendation

**B**, with the span from **C** added to `Selection` so a reader can see how old the agreement is without a new status.

B is the smaller change than it looks: it reorders existing reasoning rather than introducing new judgement. The simultaneity window, the grouping and the total order are all already there, and the constant does not change. What changes is that the window is consulted before the vote instead of after it.

It should be a `SelectChunkWithin` change with the policy name updated, because `PolicyCorroboratedFirst` will no longer describe what happens, and a snapshot manifest records its policy: an archive exported before and after should be able to say which rule produced it.

## Consequences if B is accepted

- Chunks whose most recent observations disagree with an older majority will report `superseded` and select the recent state. That is the visible behaviour change and the point of the change.
- `Summary.Corroborated` falls and `Summary.Superseded` rises on any archive with revisits. Documented figures need re-measuring rather than adjusting.
- Every observation stays exactly where it is. This changes which state is selected and what it is called, and nothing about what is stored; rejected groups are returned as evidence today and still would be.
- The policy string changes, so a snapshot manifest from before and after is distinguishable. Manifests already carry `Policy` for this reason.
- ADR 0002 said this project distinguishes absence of evidence from an observed value. This is the same principle in time: agreement that has been contradicted more recently is not evidence about the present.

## Consequences if A is accepted

The trust model gains a paragraph saying that `corroborated` means agreement among contributors' most recent observations, of any age, and that a chunk observed to have changed can still report the older state when more contributors last saw it. Written plainly, that paragraph is the argument for B.
