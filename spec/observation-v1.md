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

**Trimmed** means removing these six bytes, and no others, from both ends: `0x20` space, `0x09` tab, `0x0A` line feed, `0x0B` vertical tab, `0x0C` form feed, `0x0D` carriage return. **Lower-cased** means mapping `A`–`Z` to `a`–`z`, and nothing else.

Neither may be delegated to a language's own idea of whitespace or of case, for the same reason the timestamp below is not formatted as text. Java's `String.toLowerCase` is locale-sensitive: under a Turkish locale an upper-case `I` lowercases to a dotless letter rather than to `i`, so the same server name yields a different identity on a machine configured differently. Go's `strings.TrimSpace` removes U+00A0 and U+0085 while Java's `String.trim` stops at U+0020 and `Character.isWhitespace` excludes both, so one label ends in three different places. Both behaviours are also tied to whichever Unicode revision a runtime was built against, so two conforming implementations can drift apart without either changing a line. The definitions above depend on nothing that moves.

One consequence is worth stating rather than discovering: a name outside ASCII is not case-folded, so two spellings of one are two different servers. That is deliberate. Refusing such a name would close nothing, because a name no implementation folds is already reproducible, and it would cost somebody the ability to record what they saw.

`server_id`, `dimension`, `source.contributor` and `protocol` must be valid UTF-8. That is the one thing an implementation may be able to hold in a string that the record the identity is written onto cannot: a JSON encoder substitutes a replacement character for an invalid byte, and the identity would then not match its own record.

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
