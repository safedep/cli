bin := "bin/safedep"

build:
    go build -o {{bin}} ./cmd/safedep

test:
    go test ./...

lint:
    golangci-lint run

fmt:
    go fmt ./...

deps:
    go mod download && go mod tidy

clean:
    rm -rf bin/

release-snapshot:
    goreleaser release --snapshot --clean
