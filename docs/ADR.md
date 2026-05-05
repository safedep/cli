# ADR

Architectural decisions for the SafeDep CLI. Each decision is recorded with minimal rationale. Operational rules and the contributor walkthrough live in [DEVGUIDE.md](./DEVGUIDE.md).

## Role

The CLI is the unified DevEx layer over SafeDep open source tools ([vet](https://github.com/safedep/vet), [pmg](https://github.com/safedep/pmg), [gryph](https://github.com/safedep/gryph)) and [SafeDep Cloud](https://docs.safedep.io/cloud/overview). The goal is for users to install only this CLI and reach every SafeDep capability through it.

**Non-goal:** the CLI is not a security scanner. It orchestrates and presents; the analysis stays in upstream tools.

## Command shape

Every command follows `safedep <noun> [<noun>...] <verb>`. Rationale: a predictable shape lets humans and AI agents discover and compose commands without memorising special cases.

## Authentication

SafeDep Cloud uses two auth flows: JWT for the control plane, API key for the data plane. The CLI exposes both through a single credential layer so tools like `vet` and `pmg` reuse credentials without re-prompting users.

Credentials are stored in the system keychain via [`dry/cloud`](https://github.com/safedep/dry). When a keychain is unavailable, users supply credentials through environment variables. Plain-text credential files are not supported. Rationale: credentials are sensitive; keychain is the only widely-available secure store.

A *profile* is a named credential slot in the keychain (provided by `dry/cloud`). One profile holds one set of credentials and one tenant binding. The active profile is selected via `--profile`, `SAFEDEP_PROFILE`, persisted default, or built-in `"default"`. Rationale: users routinely have multiple SafeDep tenants; profiles let them switch without re-authenticating.

## Storage

Local CLI state uses sqlite (CGO-free `modernc.org/sqlite`) via `dry/db`. Rationale: the CLI's state is small, single-user, and shipping sqlite is friction-free.

Daemon-mode commands (e.g. continuous sync to an external endpoint) may run in environments where operators prefer PostgreSQL or MySQL. Such commands must access storage through a repository-pattern interface so the backend can be swapped. The repository abstraction is introduced when the first daemon-mode command lands; until then storage is sqlite only. Rationale: the swap need is real but distant; repository pattern at the boundary is enough.

## Configuration

Configuration is resolved with the following precedence, lowest to highest: persisted CLI configuration, configuration file (env/flag-supplied), environment variables, command-line flags. Rationale: established CLI convention; lets users layer overrides without surprises.

## User interface

Terminal presentation uses [`dry/tui`](https://github.com/safedep/dry/tree/main/tui). Rationale: the SafeDep tool family should look and feel consistent; a shared library is the only way to keep that true as tools evolve independently.

Two distinct concerns:

- **Messaging** (Info / Success / Warning / Error). dry/tui auto-detects whether to render decorated text for humans or terse, token-optimised text for AI agents based on TTY state, env vars (`CLAUDE_CODE`, `ANTHROPIC_AGENT`, `CI`, `TERM=dumb`), and `SAFEDEP_OUTPUT`. The CLI does not influence this from `--output`.
- **Data presentation** (Renderable). The `--output` flag selects `table | plain | json`. `table` is the rich, decorated variant for humans; `plain` is for shell pipelines and basic terminals; `json` is for programmatic and AI-agent consumers. When `--output` is empty, the mode auto-resolves: dry/tui rich -> `table`, plain -> `plain`, agent -> `json`.

Rationale: messaging and data have different audiences and different optimal formats. Coupling them through a single flag forces uncomfortable choices (e.g. piping JSON to `jq` should not also strip stderr decoration).

## Tool orchestration

Upstream SafeDep tools are invoked as subprocesses, not linked as libraries. The CLI manages installation and version pinning of upstream tools in a managed cache. Rationale: tools have independent dev and release cycles; linking would couple the CLI to their dependency graphs and bloat the binary.

Library-mode integration is reserved for cases where the upstream tool offers a clean, stable Go API and the dependency cost is acceptable. Decided per tool, not project-wide.

## Internal SDK

- `internal/` — CLI-only code; not importable outside this module.
- `pkg/` — CLI's own public API; populated only when external consumers exist.
- `github.com/safedep/dry` — code shared across SafeDep tools.

Rationale: clear visibility scoping prevents accidental coupling and keeps the boundary between CLI-specific and shared code unambiguous.

## Documentation

Every leaf command has a documentation page at `docs/cmd/<name>.md`, indexed from the README. Rationale: discoverability for users and AI agents; if a command is not documented it does not exist.
