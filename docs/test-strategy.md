# Test strategy

WorldLedger has two classes of compatibility to protect: archive identity and Minecraft capture fidelity.

## 1. Identity tests

These tests are release blockers.

- observation-id golden vectors;
- state-digest golden vectors;
- content-addressed object hashes;
- canonical Minecraft component golden vectors;
- capture-bundle import idempotency.

A change to a golden value requires an explicit specification review. Updating expected hashes merely to make CI pass is not acceptable.

## 2. Cross-language canonicalization

The Java adapter and a language-neutral/reference fixture generator must agree byte-for-byte.

Fixtures should include at least:

- an all-air section;
- a high-palette block section;
- property ordering that differs from runtime declaration order;
- negative section Y;
- mixed biomes;
- empty block-entity list;
- nested compounds with deliberately shuffled keys;
- NBT lists and numeric arrays;
- float/double special values represented by raw bits.

Expected fixture bytes and SHA-256 digests live in the repository and are never generated during the test itself.

The committed Java-to-Go ready bundle under `testdata/e2e-capture-bundle` is also immutable during normal tests. Java regenerates it in a temporary directory and compares every file byte-for-byte; Go then imports the committed bundle twice and checks fixed component, state, and observation identities.

## 3. Bundle ingress tests

Test successful and hostile bundles:

- multiple components;
- duplicate import;
- wrong size;
- wrong digest;
- missing file;
- absolute path;
- `..` traversal;
- symlink escape where the platform supports it;
- truncated manifest;
- oversized component;
- interrupted import.

After any failed import, `worldledger fsck` must still pass for the destination archive.

`FuzzParseManifest` continuously exercises the strict JSON and manifest boundary from valid, duplicate-key, over-nested, and invalid-UTF-8 seeds. Parser changes should also receive a bounded local fuzz run, for example:

```sh
go test ./internal/bundle -run '^$' -fuzz '^FuzzParseManifest$' -fuzztime=30s
```

## 4. Fabric unit tests

Keep canonical encoders separate from Minecraft event plumbing so they can be tested with deterministic model inputs.

Event/spool code must test:

- dirty-chunk coalescing;
- monotonic capture sequence;
- final flush on disconnect/dimension transition;
- bounded queue backpressure;
- temporary bundle recovery after simulated interruption;
- no filesystem writes on the client render/network thread beyond bounded enqueue work.

Pure-Java tests cover sequence monotonicity/exhaustion, due and final dirty claims, queue capacity and order, known-empty versus unknown block-entity state, strict temporary-bundle recovery, no-overwrite publication, and cross-instance spool locking. The client-thread/filesystem separation is structural: Minecraft callbacks only construct semantic `CaptureJob` values and call bounded `offer`; canonical encoding, hashing, and filesystem APIs are confined to the writer sink and bootstrap thread. The live fixture remains the acceptance test for actual event ordering.

## 5. Vanilla integration world

Maintain a small controlled 26.2 server fixture with known coordinates containing:

- air/stone terrain;
- block states with several properties;
- multiple biomes if practical;
- signs or another block entity with visible update NBT;
- a container whose contents are intentionally not opened during one capture pass.

Test procedure:

1. join with a clean adapter spool;
2. visit the fixture chunks;
3. perform a known block update sequence;
4. disconnect;
5. import every ready bundle;
6. run `worldledger fsck`;
7. compare expected observation/component digests;
8. verify that unopened container contents are not asserted by v1 data.

The versioned procedure and evidence template live in `examples/minecraft-26.2-fixture`. A written procedure is not a passing integration run; Windows and Linux evidence records are required before the Java capture milestone is marked complete.

## 6. Release gates

Run from the repository root:

```sh
gofmt -l $(find . -type f -name '*.go')
go run ./cmd/mcjava-fixtures
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
```

Then run with Java 25:

```sh
cd adapters/fabric
./gradlew --no-daemon clean build --warning-mode all
```

CI runs all Go gates on Linux and separately tests, vets, and builds the Windows-specific filesystem and locking paths. The Fabric build runs in Linux CI and is also verified locally on Windows; the live cross-platform client records remain a separate release gate.

## 7. Performance guardrails

Benchmarks measure canonicalization and spool throughput separately, in `internal/mcjava/bench_test.go` and `internal/bundle/bench_test.go`. Both run over committed fixtures and the committed capture bundle, so their inputs are real component sizes rather than shapes chosen to look fast.

Run them with `make bench`. They are deliberately not part of `make check`: timings vary with the machine and with what else it is doing, so a gate built on them would fail for reasons unrelated to the change under test. Recorded figures live in [`status.md`](status.md), with the machine they came from.

Initial engineering guardrails, not protocol guarantees:

- game thread performs bounded work and never waits on archive ingestion;
- dirty updates to the same chunk are coalesced;
- canonicalization queue has a hard memory bound;
- a slow disk results in backpressure/diagnostics rather than unbounded heap growth.
