# Minecraft Java chunk canonicalization v1

**Schema id:** `worldledger.minecraft.java.chunk/v1`

This specification defines the canonical byte representation produced by Minecraft Java capture adapters for client-observable chunk state. It is deliberately independent of Fabric classes, packet class names, runtime palette layouts, and Anvil serialization.

The format represents **what the client was given enough information to know**. It is not a statement about hidden server state.

## Scope

Version 1 covers:

- dimension build-range shape;
- block states for 16×16×16 chunk sections;
- biome samples for 4×4×4 biome sections;
- block-entity update data visible to the client.

Version 1 does not cover:

- entities;
- player data;
- container inventories learned by opening a screen;
- scheduled ticks;
- lighting arrays;
- heightmaps;
- structures or world-generation metadata;
- server plugin databases.

Those may be added as separately versioned components. They must not be synthesized into v1 components.

## Observation component names

A Java chunk observation uses the following component names:

```text
mcjava.shape
mcjava.blocks.<section_y>
mcjava.biomes.<section_y>
mcjava.block_entities
```

`<section_y>` is the signed decimal section coordinate with no leading `+` or zero padding. Examples:

```text
mcjava.blocks.-4
mcjava.blocks.0
mcjava.biomes.19
```

A missing component means **unknown / not asserted**. It must never be interpreted as an empty component.

Consequently:

- an all-air section is represented by a present block-section component whose palette contains `minecraft:air`;
- a chunk known to have no client-visible block-entity update records is represented by a present `mcjava.block_entities` component containing zero entries;
- if the adapter cannot establish a block-entity baseline, `mcjava.block_entities` is omitted.

## Common binary primitives

All integers are big-endian. Strings are UTF-8.

```text
u8      := 1 byte unsigned integer
u16     := 2 byte unsigned integer
u32     := 4 byte unsigned integer
i8      := 1 byte signed two's-complement integer
i16     := 2 byte signed two's-complement integer
i32     := 4 byte signed two's-complement integer
i64     := 8 byte signed two's-complement integer
f32bits := raw IEEE-754 binary32 bits encoded as u32
f64bits := raw IEEE-754 binary64 bits encoded as u64
string  := u32(byte_length) || UTF-8_bytes
bytes   := u32(byte_length) || raw_bytes
```

Decoders must reject trailing bytes unless a future version of that component explicitly allows extensions.

## Resource locations

Minecraft registry identifiers are encoded as their canonical namespaced resource location, for example:

```text
minecraft:stone
minecraft:plains
examplemod:marble
```

An adapter must not persist runtime numeric registry ids as canonical identifiers.

If a value cannot be resolved to a stable resource location, the adapter must treat that component as unsupported rather than inventing an identifier.

## Block-state strings

A block state is encoded as a single canonical string:

```text
<block_resource_location>
```

when the state has no properties, or:

```text
<block_resource_location>[<name>=<value>,<name>=<value>,...]
```

when properties are present.

Rules:

1. property names are sorted by bytewise UTF-8 lexicographic order;
2. property values use Minecraft's canonical property value names, not `toString()` on arbitrary objects;
3. no whitespace is permitted;
4. the block resource location is always namespaced.

Examples:

```text
minecraft:stone
minecraft:oak_log[axis=y]
minecraft:oak_stairs[facing=north,half=bottom,shape=straight,waterlogged=false]
```

The adapter must derive these strings from the semantic block state. It must not reuse the client's internal palette order.

## Shape component

Component name: `mcjava.shape`

Preimage:

```text
string("worldledger.minecraft.java.chunk-shape/v1")
i32(min_section_y)
u32(section_count)
```

`section_count` must be greater than zero. The valid section coordinates are:

```text
min_section_y ... min_section_y + section_count - 1
```

This component describes how section coordinates in the observation are interpreted. It does not include the chunk X/Z coordinates or dimension id; those belong to the observation envelope.

## Block-section component

Component name: `mcjava.blocks.<section_y>`

Preimage:

```text
string("worldledger.minecraft.java.block-section/v1")
i32(section_y)
u16(palette_count)
for each palette entry in canonical order:
    string(block_state)
for i in 0..4095:
    u16(palette_index[i])
```

### Palette construction

The canonical palette contains each distinct block-state string exactly once, sorted by bytewise UTF-8 lexicographic order.

`palette_count` must be in `1..4096`.

### Position order

The 4096 palette indices are written with X varying fastest, then Z, then Y:

```text
for y = 0..15:
    for z = 0..15:
        for x = 0..15:
            write state at local (x, y, z)
```

Equivalently, the linear index is:

```text
(y << 8) | (z << 4) | x
```

The representation is intentionally uncompressed. Compression belongs below the content-addressed storage boundary and must not affect canonical hashes.

## Biome-section component

Component name: `mcjava.biomes.<section_y>`

Preimage:

```text
string("worldledger.minecraft.java.biome-section/v1")
i32(section_y)
u16(palette_count)
for each palette entry in canonical order:
    string(biome_resource_location)
for i in 0..63:
    u16(palette_index[i])
```

The canonical palette contains each distinct biome resource location exactly once, sorted by bytewise UTF-8 lexicographic order.

The 64 samples use the same axis order as block sections, at Minecraft's 4×4×4 biome resolution:

```text
for y = 0..3:
    for z = 0..3:
        for x = 0..3:
            write biome sample at local quart position (x, y, z)
```

If the client exposes a biome value without a stable registry key, the adapter must omit the affected biome component and report a local diagnostic.

## Canonical NBT v1

Block-entity network data is encoded with a deterministic NBT representation. This is **not** standard NBT file encoding and must not be passed directly to vanilla readers.

A canonical NBT value is:

```text
u8(tag_type) || tag_payload
```

Tag ids follow the standard NBT tag ids:

```text
0  End          (not valid as a standalone value)
1  Byte         i8
2  Short        i16
3  Int          i32
4  Long         i64
5  Float        f32bits
6  Double       f64bits
7  Byte Array   u32(count) || count raw bytes
8  String       string
9  List         u8(element_type) || u32(count) || repeated element payloads
10 Compound     u32(count) || repeated (string(key) || canonical value)
11 Int Array    u32(count) || repeated i32
12 Long Array   u32(count) || repeated i64
```

Compound entries are sorted by bytewise UTF-8 lexicographic order of their keys. List order is preserved exactly.

For float and double tags, adapters must encode raw IEEE-754 bits rather than a decimal string representation.

Canonical NBT has no named root tag and no compression wrapper.

## Block-entity component

Component name: `mcjava.block_entities`

The component contains the most recent **server-supplied client-visible block-entity update representation** known to the adapter for the chunk at the observation point.

The adapter must not create this component by serializing arbitrary current `BlockEntity` objects and treating default client fields as server knowledge. In particular, a container object with no locally known inventory must not become evidence that the server-side inventory is empty.

Preimage:

```text
string("worldledger.minecraft.java.block-entities/v1")
u32(entry_count)
for each entry in canonical order:
    u8(local_x)
    i32(block_y)
    u8(local_z)
    string(block_entity_type_resource_location)
    bytes(canonical_nbt_value)
```

Entries are sorted by `(block_y, local_z, local_x, block_entity_type_resource_location)` in ascending order.

`local_x` and `local_z` must be in `0..15`.

The embedded canonical NBT value must have tag type `10` (Compound).

The NBT payload is the data supplied to the client by the relevant chunk/block-entity network updates. No attempt is made in v1 to infer unsent fields. Standard metadata duplicated by the network representation may remain present; v1 does not strip keys such as `id`, `x`, `y`, or `z`.

## Completeness predicates

Completeness is derived from component presence, not from substituting default values.

A chunk observation is **terrain-complete for its declared shape** when:

1. `mcjava.shape` is present; and
2. every valid section coordinate has a corresponding `mcjava.blocks.<section_y>` component.

It is **biome-complete for its declared shape** under the equivalent rule for `mcjava.biomes.<section_y>`.

It has a **block-entity network baseline** when `mcjava.block_entities` is present.

These predicates do not imply that the client knows hidden server state.

## Observation protocol string

An adapter implementing this specification for a concrete game release must use:

```text
minecraft-java/<game_version>;canonical=worldledger.minecraft.java.chunk/v1
```

For the first supported release:

```text
minecraft-java/26.2;canonical=worldledger.minecraft.java.chunk/v1
```

There is no whitespace in the protocol string.

## Snapshot semantics

A canonical observation is produced after the client has applied the network state that caused the snapshot.

Adapters should coalesce bursts of block updates, but must preserve causal order within one capture session. A final dirty snapshot should be attempted before a chunk is discarded or a dimension/session ends.

`observed_at` is the local wall-clock time associated with the completed snapshot. It is not asserted to be server time.

## Security and resource bounds

Canonicalization operates on data originating from an untrusted multiplayer server. Implementations must bound memory use and recursion even when the vanilla client has already accepted the packet.

At minimum:

- reject malformed component dimensions;
- bound NBT recursion depth;
- bound total canonical NBT bytes per block entity;
- bound total canonical bytes produced per chunk observation;
- never use a resource location, NBT key, or server-provided string as a filesystem path.

Concrete limits are adapter policy and are not part of the canonical hash semantics. If a limit is hit, omit the affected component and retain a local diagnostic rather than emitting a fabricated value.

## Versioning rule

Any change that can alter canonical bytes for the same semantic client-observable state requires a new schema id.

Bug fixes that reveal a previous implementation did not conform to this document do not change the schema id; the broken implementation must be identified by its capture-agent version and affected observations must not be silently rewritten.
