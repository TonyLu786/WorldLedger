# Adapter versioning

The capture adapter is pinned to one Minecraft release. That is a decision, not an oversight, and it has a maintenance cost that has to be paid deliberately rather than discovered later.

## Why the adapter is pinned and the core is not

The Go core has no Minecraft dependency at all. It reads canonical bytes defined by [`../spec/minecraft-java-chunk-v1.md`](../spec/minecraft-java-chunk-v1.md), which name blocks and biomes by resource location and never store a runtime registry id. An archive written today stays readable when Minecraft changes, because nothing in it depends on Minecraft's internals.

The adapter is the opposite. It reads live client state through mixins into classes that Mojang renames, moves, and restructures. Every release can break it.

This split is the point: **protocol churn is absorbed at the edge so that the archive format never has to change.**

## What a Minecraft release can break

| Break | Detected by |
| --- | --- |
| Mixin target moved or renamed | Compile failure |
| Chunk or block-entity access changed | Compile failure |
| Packet handler signature changed | Compile failure |
| Build range changed | `mcprofile` output differs |
| Block or biome renamed | `dfurenames` output differs |
| World directory layout changed | Live client run only |
| Canonical bytes changed for the same observed state | Golden fixtures |

The last row is the dangerous one. The first three fail loudly. A change that quietly alters canonical bytes for state that did not change would split the archive's identity, which is why golden fixtures exist and why changing one requires a written explanation.

## Supporting a new release

1. **Profile it.** Run `cmd/mcprofile` against the new client jar. A changed data version, build range, block registry, or structure placement is now data rather than a guess.
2. **Extract its renames.** Run `cmd/dfurenames`. Its coverage report names the fixers it could not read, so the gaps are visible.
3. **Re-pin the build.** Update `adapters/fabric/gradle.properties`. Every value there is an exact build input.
4. **Compile.** Most breakage surfaces here.
5. **Run the golden fixtures.** `go run ./cmd/mcjava-fixtures` and the Java canonical tests must both pass unchanged. If they do not, stop: either the canonicalisation changed, which is a schema event, or the adapter is producing wrong bytes.
6. **Run the client game test.** `./gradlew runClientGametest`. Compilation says the code links; only a live run says the capture hooks still fire in the right order.
7. **Check the world layout.** 26.2 moved every dimension under `dimensions/<namespace>/<path>/region/`. `anvil.DimensionDirectory` probes the world rather than assuming, so a future move is absorbed if the new layout is added there, but it has to be noticed first.

## Rules that do not bend

- **A new release must not change canonical bytes for the same observed state.** If it does, that is a new schema id, not an edit to `worldledger.minecraft.java.chunk/v1`. Old observations stay valid under the old schema.
- **A golden value is never updated to make a build pass.** It is updated only with an explanation of what changed in the world and why the new value is correct.
- **The protocol string records the release that was captured.** `minecraft-java/<version>;canonical=<schema>` travels with every observation, so an archive spanning releases stays interpretable.

## Supporting several releases at once

Not currently done, and worth being explicit about the shape it would take rather than leaving it implied.

Loom builds one adapter against one Minecraft version. Covering several means either separate adapter modules sharing the canonical encoder, or a multi-version build. The canonical encoder is already isolated from Minecraft classes and tested against version-neutral fixtures, so it is the part that would be shared unchanged.

The archive needs no changes for this: observations from different releases already coexist, because the schema id and the protocol string are per observation.

## Practical expectation

The adapter will lag new Minecraft releases. An archive written by an older adapter stays valid and importable, and the core keeps working, so a lagging adapter stops new capture rather than invalidating what exists.

That is the property worth protecting. Losing capture for a few weeks after a release is an inconvenience; losing the ability to read what was already captured would not be.
