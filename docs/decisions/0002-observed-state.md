# ADR 0002: Preserve observed state, not inferred server state

**Status:** accepted

## Context

A multiplayer client receives only the state the server chooses to expose. Some information is complete enough for rendering, while other information is intentionally absent or only supplied after interaction. Container inventories are a common example.

Serializing the current client object graph can turn an unknown field into a default value, producing false historical claims.

## Decision

Canonical capture formats distinguish absence of evidence from an observed empty value.

For Java chunk v1:

- terrain and biome values come from the applied client chunk state when the adapter has a full chunk baseline;
- block-entity NBT comes from server-supplied block-entity network update data, not from a generic client-side save of arbitrary block-entity objects;
- a component is omitted when the adapter cannot establish the knowledge required by that component's specification;
- no component in v1 contains container inventory contents learned from opening a container.

## Consequences

An exported historical world may contain less state than existed on the server. That limitation is preferable to fabricating certainty.

Future container, entity, and other component formats can be added with their own observation scopes without redefining the meaning of v1 data.
