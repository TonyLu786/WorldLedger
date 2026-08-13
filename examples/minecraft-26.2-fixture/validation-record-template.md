# Minecraft 26.2 validation record

## Environment

```text
Date/time (UTC):
Operating system:
Java vendor/version:
Minecraft client:
Minecraft server:
Server JAR SHA-256:
Fabric Loader:
Fabric API JAR SHA-256:
WorldLedger commit:
WorldLedger mod JAR SHA-256:
Go version:
```

## Configuration

```text
server.properties differences:
capture.properties (redact nothing except unrelated local paths):
Fixture setup command result:
Spool clean-start evidence:
```

## Capture evidence

```text
Session start/end:
Ready bundle names in sequence order:
Temporary bundles after shutdown:
Quarantine bundles after shutdown:
Backpressure events:
Dropped final coverage:
Snapshot/spool failures:
Preserved client log path and SHA-256:
Preserved spool archive path and SHA-256:
```

## Import evidence

Paste the complete command output for:

```text
worldledger init
every worldledger ingest-bundle invocation
duplicate import of one ready bundle
worldledger fsck
worldledger inspect for chunk 0,0
```

## Semantic transition table

| Step | Bundle | State digest | Blocks digest(s) | Biomes digest(s) | Block-entities digest | Notes |
|---|---|---|---|---|---|---|
| Baseline | | | | | | |
| Log axis z | | | | | | |
| Repeater mutation | | | | | | |
| Forest biome | | | | | | |
| Sign text B | | | | | | |
| Final disconnect | | | | | | |

## Privacy boundary checks

```text
minecraft:sign block-entity type present:
chest identifier absent:
Items key absent:
chat/auth/player/entity data absent from every bundle:
```

## Cross-platform comparison

```text
Peer record:
Equivalent component digests match:
Expected provenance-only differences:
Unexplained differences:
```

## Result

```text
PASS / FAIL:
Reviewer:
Known defects or deviations:
```
