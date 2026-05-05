# ADR

Project goal is to provide an unified experience to SafeDep Open Source tools such as
[vet](https://github.com/safedep/vet), [PMG](https://github.com/safedep/pmg),
[gryph](https://github.com/safedep/gryph) and [SafeDep Cloud](https://docs.safedep.io/cloud/overview).

The CLI is a DevEx and user experience focussed tool. It aims to build an experience layer on top of
various SafeDep tools and cloud capabilities. Provide an unified interface for humans and AI agents
to use SafeDep.

## Non-goals

* Not a security scanner by itself but can embed / integrate with SafeDep security tools.

## Commands

All commands and sub-commands in the CLI must follow the convention below:

```
safedep [domain] [...sub-domain] [action]
```

This in turn can be thought of using the following mental model:

```
safedep [noun] [...noun] [verb]
```

Example good commands:

```
safedep auth login                # Triggers JWT login flow
safedep auth login --api-key      # Login using an API key
```

## Authentication

There are two type of authentication supported by SafeDep Cloud:

1. Token (JWT) based authentication required for control plane APIs
2. API key authentication required for data plane APIs such as those by security tools

The CLI must follow convention to securely execute authentication flows and store credentials in a
way that is usable by all SafeDep tools. Credentials must be stored in system Keychain only. When
Keychain is not available, users are explicitly required to provide credentials through environment
variables. Do not store credentials in plain text.

For example, when API key credential is configured by CLI, `vet`, `pmg` and other SafeDep tools
should automatically discover and use the credentials when required without requiring users to
configure credentials separately for each tool.

## Internal SDK

Provide SDKs as required for implementing and evolving CLI functionalities. Any SDK suitable for CLI
use only should be in `pkg` namespace. Any SDK suitable for use across different tools should be in
[DRY](https://github.com/safedep/dry).

## Storage

The CLI will use `sqlite` as the default state storage and expose APIs for individual commands.
`modernc.org/sqlite` will be used for CGO-free sqlite support. However, all storage access will be
via. GORM and following repository pattern so that the underlying `sqlite` storage can be replaced
with PostgreSQL or MySQL for production deployment of CLI.

## Configuration

The CLI will persist it's configuration using the storage APIs. The CLI will also expose internal
SDKs for commands and sub-commands to read, store, update configuration using simple storage
agnostic APIs. Additionally, CLI will support configuration overrides in the following order of
precedence:

* Persistent configuration stored in CLI storage
* Configuration file supplied via. environment variable or command line argument
* Environment variable based configuration overrides
* CLI arg based configuration overrides

## Documentation

Each command should have its own documentation page in `docs/cmd/<cmd-name>.md`. The documentation
page should describe the common use-cases without being too verbose. The `README.md` should maintain
an index of supported commands linking to the command specific documentation.

## User Interface

The CLI should use a *responsive* terminal user interface (TUI), suitable for use by both humans and
AI agents. Specifically, it should support the following response types:

* Rich text with tabular data support
* JSON and JSONL (JSON Lines) output for easy parsing by AI agents and other tools
