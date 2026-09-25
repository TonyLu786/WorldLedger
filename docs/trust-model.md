# Trust model

WorldLedger is designed for public contributions where observations may be incomplete, duplicated, delayed, mistaken, or deliberately fabricated.

The core principle is simple: **store claims immutably; derive confidence separately.**

## What an observation means

An observation is a claim that a capture source saw a particular canonical state for a particular chunk at a particular time.

It does not claim that:

- the observation is a complete copy of server-side state;
- the contributor's clock is exact;
- the server sent identical data to every client;
- another different observation is fraudulent;
- the observed state existed for the entire verification window.

## Threats

### Fabricated payloads

A contributor can upload bytes that were never received from a server. Content hashes prove object identity, not truth.

Mitigations planned for the service layer include independent corroboration, contributor keys, rate limits, reputation, capture attestations, and anomaly detection.

### Sybil contributors

One operator can create many contributor identities. A count of account names is therefore not equivalent to a count of independent witnesses.

The current core calls these `Contributors`, not `TrustedWitnesses`, and does not assign a global confidence score.

### Clock manipulation

Observed timestamps are supplied by capture sources. They can be inaccurate or malicious.

Future observations should carry clock-quality metadata and an upload receipt timestamp. Verification can then reason about intervals rather than exact instants.

[ADR 0003](decisions/0003-corroboration-and-time.md) gave a clock one more thing to decide, and it is worth naming rather than leaving to be found. Selection now consults the simultaneity window before it counts anybody, and the window is measured back from the most recent eligible observation. A contributor whose clock runs fast moves it. Observations that were genuinely simultaneous with theirs fall outside and stop deciding what the chunk holds: they still corroborate if they agree, and if they disagree they become what the chunk used to hold rather than a contradiction. **A clock wrong by more than the window can therefore turn a conflict into a change**, which is a disagreement being hidden rather than manufactured.

Three things bound it, and none of them is a defence:

- it takes a skew larger than the window, thirty seconds by default, before anybody moves out of it;
- an observation claiming a time after the epoch is not eligible at all, so for an export of "now" a fast clock excludes only itself;
- it can narrow who decides, never add a voter, so the contributor doing it cannot make anybody agree with them.

What is done about it is that it is counted. Every `Selection` carries how many eligible observations were inside the window and how many fell outside, so a reader can see that a chunk was decided by one observation while four others were set aside. That is the same shape as everything else here: the archive cannot verify a clock, and it can refuse to be quiet about what a clock decided.

### Legitimate disagreement

Minecraft worlds are mutable. Two conflicting observations seconds apart may both be correct.

For this reason, the core preserves all states and labels the window `conflict` rather than selecting a winner.

### Incomplete client visibility

Server plugins, anti-xray systems, view distance, unloaded entities, unopened containers, and protocol behavior can limit what a client can know.

Canonical formats must distinguish "unknown" from a known empty/default value. Missing data must never be normalized into fabricated certainty.

### Inference from published observations

Every threat above concerns what an observation claims. This one concerns what an observation *reveals* without claiming it.

Minecraft generates its world from a seed. Anyone holding enough observed chunks can search for the generation parameters that reproduce them. The techniques are public and mature: structure placement, biome samples, terrain shape, ore and decorator placement, and the deep bedrock transition are all evidence, and existing tools already consume them.

This matters because of what a recovered seed hands over. It is not one more fact about the world; it is the world's entire unexplored future. Every stronghold, ancient city, buried treasure, slime chunk, and spawner becomes computable, including in regions nobody has ever visited.

Three properties make this different from the other threats:

- **It is a property of the data, not of a feature.** Publishing the observations publishes the recoverability, whether or not this project ever ships a recovery tool. The archive is the raw material.
- **The harm lands on people who never contributed.** A server's operators and its other players did not consent, and are usually not even aware an archive exists.
- **It is irreversible.** An observation can be withdrawn from a public archive; a seed that has been recovered from it cannot be withdrawn from the people who have it.

The asymmetry is unfavourable to the archive: recovery needs one sufficiently covered region, while protection must hold across every published chunk. Reducing per-chunk detail does not fix this, because coverage substitutes for detail.

Consequences for the project:

- Coverage-level publication controls belong *before* a public archive exists, not after. A server that has not agreed to publication should not be publishable at whole-region granularity.
- Aggregation is the risk. Individually harmless observations become a seed when merged, so publication policy has to be evaluated over the merged archive, not over each contribution.
- Worlds using a hardened generator (a large or cryptographically derived seed) are outside this threat, and worlds with custom generation may have no recoverable seed at all. Neither can be assumed.
- The reverse-engineering capability that ships with this project is deliberately gated and attributed; see [`docs/seed-recovery.md`](seed-recovery.md). Gating a tool does not reduce the risk described here, which comes from the data.

Secrecy of a seed is a weak protection and was never designed as a security boundary. That is a reason to be careful about publishing archives, not a reason to treat the exposure as acceptable.

## What signatures changed, and what they did not

A contributor label was a string an adapter wrote into a bundle. `worldledger attest` binds an observation to an ed25519 key instead. The signature covers the observation id, which is a digest over the schema, server, dimension, chunk, instant, protocol, contributor label, and state digest, so a signature cannot be lifted onto a different record, chunk, moment, or claimed author.

That closes fabricated attribution, and only that. A signature proves the holder of a key asserted something; it does not make the assertion true, and generating a key costs nothing. The threats above are unchanged by it: a Sybil contributor can hold as many keys as they like, and a fabricated payload signed by a real key is still fabricated.

The registry of known identities is where judgment enters, and it is deliberately manual. Registering a key names who decided to trust it. A label already held by one key cannot be taken by another, because silently accepting the second would let anyone who generates a key inherit the standing of the name they picked. Removing an identity does not invalidate signatures already made with it; they remain valid signatures from a key this archive no longer recognises, which is a different statement from a forgery and is reported as one.

## Publication policy

The software can technically archive data that a multiplayer client receives. Public archive operators still need publication rules appropriate to their community, jurisdiction, and server context.

The project should support server- and collection-level embargoes, contributor deletion of account metadata where feasible, and separation between raw observations and curated public landmarks. Those are service policies, not reasons to weaken the integrity of the underlying archive format.

## What withdrawal can and cannot undo

A contributor can ask for their observations to be withheld, and a server operator can ask for an area to be excluded. `worldledger redact` records either as an attributed, dated, reversible declaration, and anything the archive builds for sharing skips the observations it covers — including their component bytes, since dropping the record and keeping the bytes would be the same disclosure with an extra step.

That sentence was true of everything except the one path that actually hands data to somebody else. `send` assembled its bundle from every observation and negotiated its objects from an unfiltered fingerprint, so a contributor who had withdrawn consent still travelled, record and bytes, with nothing printed. It filters now, refuses to carry an object that only a withheld record needs, and reports the count it held back. `fingerprint` and `manifest` still describe everything, and that stays. Both are written to be handed to a peer so that peer can work out what to send, and one filtered down to what its author was willing to share would have them send bytes the archive already holds or skip chunks it does not. It is still a disclosure: a fingerprint says which chunks were seen and in what states, so an operator sharing one is sharing a little about what was withdrawn.

What was wrong was where that was written down. This paragraph was the only place it appeared, and nobody exporting a fingerprint has it open. Both commands now say it themselves when the archive has redactions declared, on standard error so that a manifest stays pipeable, and both refuse outright when the redaction records cannot be read. A program about to hand somebody a file should not be unable to say what was meant to be kept out of it. `redact set` already listed coverage, export, convert and send by name and did not claim these two, which is why nothing here was untrue; it was unsaid.

Removing the underlying bytes is a different matter, and the difference is not a limitation of the implementation. Objects are stored by content, so two contributors who observed the same chunk in the same state reference one object. When one of them withdraws, that object is still what the other one saw. Deleting it would destroy an observation that was never theirs to withdraw.

This is the ordinary case rather than a corner. In the first real archive here holding two contributors, withdrawing one removed 40 observation records and zero bytes: every state they had seen had been independently observed by the other. `redact purge` reports each object it kept and names the surviving contributor who still references it.

The consequence is worth stating plainly to anyone who asks to be forgotten. What a contributor can withdraw is their own claim to have seen something: their name, their timing, their attestation. What they cannot withdraw is the fact of the world's state, once someone else has independently recorded the same thing. An archive built on corroboration cannot offer more than that without lying, and the exposure that actually matters here comes from accumulated coverage rather than from any one contributor's records.
