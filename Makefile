BIN := bin/safedep

.PHONY: build test lint lint-conventions fmt deps clean release-snapshot

build:
	go build -ldflags "-s -w" -o $(BIN) ./cmd/safedep

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
