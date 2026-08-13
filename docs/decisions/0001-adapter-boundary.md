# ADR 0001: Keep capture adapters outside the archive core

**Status:** accepted

## Context

Minecraft client internals change much faster than the semantics of an archive. Java Edition 26.1 also removed obfuscation and changed the recommended Fabric development mapping/tooling model, which reinforces that game-facing code is a volatile boundary.

The archive core needs to remain usable by Fabric clients, protocol proxies, import tools, and eventually Bedrock adapters without inheriting their runtime dependencies.

## Decision

Capture adapters do not link the Go archive core into the game process.

For the first Java implementation, the Fabric adapter writes crash-safe capture bundles as defined by `spec/capture-bundle-v1.md`. The Go core imports and verifies those bundles.

The adapter owns:

- Minecraft/Fabric integration;
- detection of client-observable changes;
- canonicalization into versioned component bytes;
- local spool durability.

The core owns:

- content addressing;
- observation identity;
- archive indexes;
- integrity checks;
- cross-contributor comparison;
- later merge/export policy.

## Consequences

Benefits:

- a game crash cannot corrupt the archive database through an in-process binding;
- Java and Go can be tested independently against shared golden fixtures;
- adapter upgrades do not require changing archive storage code;
- failed imports can be retried from the spool;
- the same ingress path can be reused by non-Fabric producers.

Costs:

- there is an additional local interchange format;
- near-real-time ingestion requires a small watcher/import process later;
- provenance that is bundle-only must be promoted deliberately when the archive observation schema evolves.
