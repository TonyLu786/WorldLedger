# Observation v1

**Schema id:** `worldledger.observation/v1`

This document defines the metadata identity rules implemented by the archive core. It does not define Minecraft chunk canonicalization; each canonical payload format requires its own versioned specification.

## Fields

```text
schema
id
chunk.server_id
chunk.dimension
chunk.x
chunk.z
observed_at
received_at
protocol
source.contributor
source.agent
components[name] -> { algorithm, digest, size }
state_digest
```

## Normalization

`server_id` and `dimension` are trimmed and lower-cased before identity calculation. Contributor and protocol strings are trimmed but retain case.

Timestamps used for identity are converted to UTC and encoded as two integers: seconds since the Unix epoch, then nanoseconds within that second.

They are never formatted as text for identity purposes. Every textual form of an instant leaves a choice about trailing zeros in the fractional part, and implementations resolve it differently — one language writes 100 milliseconds as `.1Z`, another as `.100Z`. Both are valid RFC 3339, and both would produce a different identity for the same instant. An identity rule that depends on a formatting convention is not a rule. Integers remove the choice.

This constrains only the identity preimage. The `observed_at` field carried in a capture bundle or stored in an observation record remains an RFC 3339 string; implementations parse it to an instant and encode that instant as integers when deriving identity.

Component names are sorted by bytewise lexicographic order before state hashing.

## Hash encoding

All identity hashes use SHA-256 over an unambiguous binary preimage.

Primitive encodings:

```text
string  := uint32_be(byte_length) || UTF-8_bytes
uint32  := 4-byte unsigned big-endian integer
int32   := 4-byte two's-complement big-endian integer
int64   := 8-byte two's-complement big-endian integer
```

The resulting SHA-256 digest is represented as 64 lowercase hexadecimal characters.

## State digest

The preimage is:

```text
string("worldledger.state/v1")
uint32(component_count)
for each component in sorted-name order:
    string(component_name)
    string(algorithm)
    string(digest)
    int64(size)
```

The state digest identifies a set of component payloads, not a time or source.

## Observation id

The preimage is:

```text
string("worldledger.observation/v1")
string(normalized_server_id)
string(normalized_dimension)
int32(chunk_x)
int32(chunk_z)
int64(observed_at_utc_epoch_seconds)
uint32(observed_at_utc_nanoseconds)
string(trimmed_protocol)
string(trimmed_source_contributor)
string(state_digest)
```

`received_at` and `source.agent` are deliberately excluded. Upload latency and capture software labels must not change the identity of an otherwise identical source assertion.

## Payload requirements

The object store hashes the exact canonical bytes supplied by an adapter. An adapter MUST NOT treat semantically equivalent but byte-different encodings as interchangeable unless its canonicalization specification requires them to become byte-identical first.

Each canonical component specification must define:

- byte order;
- field order;
- registry representation;
- treatment of unknown/missing data;
- treatment of transient fields;
- Minecraft/protocol compatibility range;
- canonicalization version identifier.

Until those rules exist for a component, its digest is meaningful only within the capture implementation that produced it.
