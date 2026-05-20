GO := go
BIN_DIR := bin

# Platform detection mirrors pmg/Makefile so contributors on Windows
# (cmd.exe or Git Bash) get the right shell-out paths.
ifeq ($(OS),Windows_NT)
	BINARY_EXT := .exe
	ifneq ($(shell uname -s 2>/dev/null),)
		MKDIR_P := mkdir -p $(BIN_DIR)
		RM_RF := rm -rf $(BIN_DIR)
	else
		MKDIR_P := if not exist $(BIN_DIR) mkdir $(BIN_DIR)
		RM_RF := if exist $(BIN_DIR) rmdir /s /q $(BIN_DIR)
	endif
else
	BINARY_EXT :=
	MKDIR_P := mkdir -p $(BIN_DIR)
	RM_RF := rm -rf $(BIN_DIR)
	SHELL := /bin/bash
endif

BIN := $(BIN_DIR)/safedep$(BINARY_EXT)

# Version and commit metadata are injected at link time. Goreleaser
# overrides these via .goreleaser.yaml during release builds; local
# builds derive them from git so `safedep version` always reflects the
# actual binary.
GITCOMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
GITCOMMIT_SHORT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GITTAG := $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
VERSION := "$(GITTAG)-$(GITCOMMIT_SHORT)"

GO_CFLAGS := -X 'github.com/safedep/cli/internal/version.Commit=$(GITCOMMIT)' \
             -X 'github.com/safedep/cli/internal/version.Version=$(VERSION)'
GO_LDFLAGS := -ldflags "-s -w $(GO_CFLAGS)"

.PHONY: all build create_bin test lint lint-conventions fmt deps mocks clean release-snapshot

all: build

build: create_bin
	$(GO) build $(GO_LDFLAGS) -o $(BIN) ./cmd/safedep

create_bin:
	$(MKDIR_P)

test:
	$(GO) test ./...

lint:
	golangci-lint run

lint-conventions: lint
	$(GO) test ./internal/cmd/ -run Conventions -count=1

fmt:
	$(GO) fmt ./...

deps:
	$(GO) mod download && $(GO) mod tidy

mocks:
	$(GO) tool mockery

clean:
	$(RM_RF)

release-snapshot:
	goreleaser release --snapshot --clean
