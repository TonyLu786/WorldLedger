## Summary

Describe the change and the problem it solves.

## Archive compatibility

- [ ] This change does not alter observation identity, canonical payload bytes, archive layout, or merge semantics.
- [ ] If it does, the corresponding design proposal and migration/versioning plan are linked below.

## Verification

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `go test -race ./...`
- [ ] `go run ./cmd/mcjava-fixtures`
- [ ] Fabric changes: `cd adapters/fabric && ./gradlew --no-daemon clean build --warning-mode all`
- [ ] Capture-hook changes: the controlled Minecraft 26.2 procedure is attached, or the reason it is not applicable is stated.

List any additional fixtures or manual checks used to verify the change.
