GO ?= go
FABRIC_DIR := adapters/fabric
GO_FILES := $(shell find . -type f -name '*.go' -not -path './.git/*')

.PHONY: build test race vet fmt fmt-check fixture-check version-check bench fabric-build check clean

build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/worldledger ./cmd/worldledger

test:
	$(GO) test -count=1 ./...

race:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w $(GO_FILES)

fmt-check:
	@unformatted="$$(gofmt -l $(GO_FILES))"; test -z "$$unformatted" || { printf '%s\n' "$$unformatted"; exit 1; }

fixture-check:
	$(GO) run ./cmd/mcjava-fixtures

version-check:
	./scripts/check-documented-versions.sh

# Deliberately outside check: benchmark timings vary with the machine and with
# what else it is doing, so a gate built on them would fail for reasons that
# have nothing to do with the change under test.
bench:
	$(GO) test ./internal/mcjava/ ./internal/bundle/ -run '^$$' -bench . -benchtime=300x

fabric-build:
	cd $(FABRIC_DIR) && ./gradlew --no-daemon clean build --warning-mode all

check: fmt-check fixture-check version-check test vet race build fabric-build

clean:
	rm -rf bin dist
	cd $(FABRIC_DIR) && ./gradlew --no-daemon clean
