GO ?= go
FABRIC_DIR := adapters/fabric
GO_FILES := $(shell find . -type f -name '*.go' -not -path './.git/*')

.PHONY: build test race vet fmt fmt-check fixture-check version-check fabric-build check clean

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

fabric-build:
	cd $(FABRIC_DIR) && ./gradlew --no-daemon clean build --warning-mode all

check: fmt-check fixture-check version-check test vet race build fabric-build

clean:
	rm -rf bin dist
	cd $(FABRIC_DIR) && ./gradlew --no-daemon clean
