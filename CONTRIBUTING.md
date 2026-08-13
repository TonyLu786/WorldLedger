# Contributing

WorldLedger is in an interface-forming stage. Changes to hashing, canonicalization, observation identity, or archive layout need more scrutiny than ordinary implementation changes because public data may depend on them permanently.

## Development

Requirements:

- Go 1.23+
- Java 25 for the Fabric adapter
- Git

Run the full local check:

```sh
make check
```

Without `make`, run the release gates directly:

```sh
gofmt -l $(find . -type f -name '*.go')
go run ./cmd/mcjava-fixtures
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
cd adapters/fabric
./gradlew --no-daemon clean build --warning-mode all
```

On Windows PowerShell, check Go formatting with `gofmt -l (rg --files -g '*.go')` and use `gradlew.bat`. Pull requests that change client hooks must also state whether the controlled procedure in `examples/minecraft-26.2-fixture` was run. Automated canonical/bundle fixtures do not count as a live packet-order test.

Changes to capture-bundle parsing should receive a bounded fuzz run in addition to the normal seed corpus:

```sh
go test ./internal/bundle -run '^$' -fuzz '^FuzzParseManifest$' -fuzztime=30s
```

Pull requests should keep the build warning-free under `go vet` and include tests for behavior that changes archive semantics.

Do not regenerate committed fixture outputs during normal tests. A changed fixture hash requires a specification-level explanation and an independently reviewable generation command.

## Before changing a stable identity rule

Open an issue before changing any of the following:

- `state_digest` input fields;
- observation id input fields;
- normalization rules;
- canonical payload bytes;
- archive format version;
- merge/conflict semantics.

A proposal should include a migration story for existing archives. If no safe migration exists, the change requires a new schema or canonicalization version rather than an in-place edit.

## Commit style

Use short imperative subjects. Keep refactors separate from semantic changes where practical.

Examples:

```text
Add archive integrity scan
Reject malformed SHA-256 references
Document clock uncertainty model
```

## Scope

Good early contributions include:

- deterministic fixtures;
- corruption detection;
- performance benchmarks;
- archive import/export tools;
- protocol research with reproducible packet captures;
- documentation of edge cases in Minecraft world state.

Large UI work should wait until the contribution and archive protocols settle.
