BIN := bin/safedep

# Version and commit metadata are injected at link time via -ldflags. Goreleaser
# overrides these with its own values during release builds; local builds derive
# them from git so `safedep version` always reflects the actual binary.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)

LDFLAGS := -s -w \
	-X github.com/safedep/cli/internal/version.Version=$(VERSION) \
	-X github.com/safedep/cli/internal/version.Commit=$(COMMIT)

.PHONY: build test lint lint-conventions fmt deps clean release-snapshot

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/safedep

test:
	go test ./...

lint:
	golangci-lint run

lint-conventions: lint
	go test ./internal/cmd/ -run Conventions -count=1

fmt:
	go fmt ./...

deps:
	go mod download && go mod tidy

clean:
	rm -rf bin/

release-snapshot:
	goreleaser release --snapshot --clean
