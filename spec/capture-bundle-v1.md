# Capture bundle v1

**Schema id:** `worldledger.capture-bundle/v1`

Capture bundles are the crash-safe local interchange boundary between a capture adapter and the WorldLedger archive core.

They are not the public archive format. A bundle may be deleted after the archive core has imported and verified it.

## Directory layout

```text
<bundle>/
├── bundle.json
└── components/
    ├── ...
    └── ...
```

A producer writes the bundle into a temporary directory and performs an atomic rename to its final directory name only after every component and `bundle.json` have been flushed and closed.

Consumers must ignore temporary/incomplete directories.

## Manifest

`bundle.json` is UTF-8 JSON with this logical schema:

```json
{
  "schema": "worldledger.capture-bundle/v1",
  "server_id": "example.org:25565",
  "server_address": "example.org:25565",
  "dimension": "minecraft:overworld",
  "chunk": { "x": 14, "z": -8 },
  "observed_at": "2026-08-09T12:00:03.123456Z",
  "protocol": "minecraft-java/26.2;canonical=worldledger.minecraft.java.chunk/v1",
  "source": {
    "contributor": "alice",
    "agent": "worldledger-fabric/0.1.0-dev"
  },
  "capture": {
    "session_id": "5dfe3db2-208e-4cd8-8d11-1d83fa4f951b",
    "sequence": 417,
    "trigger": "dirty-flush"
  },
  "components": {
    "mcjava.shape": {
      "path": "components/shape.bin",
      "algorithm": "sha256",
      "digest": "...64 lowercase hex...",
      "size": 60
    }
  }
}
```

### Required fields

The following are required:

- `schema`;
- `server_id`;
- `dimension`;
- `chunk.x` and `chunk.z`;
- `observed_at`;
- `protocol`;
- `source.contributor`;
- at least one component.

`server_address`, `source.agent`, and `capture` are local provenance fields. The archive core may preserve more of this provenance in later archive schema revisions, but their presence must not affect component hashes.

## Component descriptors

Each component descriptor contains:

```text
path       relative POSIX-style path within the bundle
algorithm  currently exactly "sha256"
digest     lowercase hexadecimal SHA-256 of the exact component bytes
size       exact byte length
```

A consumer must independently verify size and digest before importing a component.

Component names are observation component names and are not filesystem paths.

## Path safety

A consumer must reject a bundle if a component path:

- is absolute;
- contains `..` path traversal;
- resolves outside the bundle directory;
- refers to a symlink or other indirection that escapes the bundle;
- resolves to the manifest itself.

The importer must open files without trusting the producer's path normalization.

## Import semantics

Bundle import is idempotent.

The archive core:

1. validates the manifest and all component descriptors;
2. verifies every component hash and size;
3. imports component bytes into CAS;
4. constructs and finalizes the normal `worldledger.observation/v1` record;
5. appends the observation to the chunk index if absent;
6. reports the resulting observation id and state digest.

An interrupted import may leave unreferenced immutable CAS objects. It must not leave a partially written observation record or chunk-index entry.

The source bundle remains intact unless the caller explicitly requests cleanup after successful import.

## Producer crash safety

The recommended producer algorithm is:

```text
create <spool>/.tmp-<uuid>/
write and fsync component files
write and fsync bundle.json
fsync temporary directory where supported
rename .tmp-<uuid> -> ready-<session>-<sequence>
fsync spool parent where supported
```

The adapter must never overwrite a ready bundle in place.

## Limits

Importers must enforce configurable bounds for:

- manifest size;
- component count;
- individual component size;
- aggregate bundle size.

Limit failures leave the source bundle untouched and return an explicit error.
