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

## Publication policy

The software can technically archive data that a multiplayer client receives. Public archive operators still need publication rules appropriate to their community, jurisdiction, and server context.

The project should support server- and collection-level embargoes, contributor deletion of account metadata where feasible, and separation between raw observations and curated public landmarks. Those are service policies, not reasons to weaken the integrity of the underlying archive format.
