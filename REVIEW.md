# Review instructions

These instructions apply only to code review. `CLAUDE.md` (`AGENTS.md`) gives the project context.
`docs/ADR.md` and `docs/DEVGUIDE.md` are the sources of truth. A change that breaks a rule in them
is a finding.

## Rules tagged (lint) in DEVGUIDE

DEVGUIDE tags some rules **(lint)**. Some of these rules have no lint yet. Treat each one as
required, even when CI passes. A violation of a (lint) rule that CI does not catch is Important.

CI already checks these (lint) rules. Do not report them:

- Command shape `safedep <noun> [<noun>...] <verb>`, the verb allow-list in `internal/cmd/verbs.go`,
  no hyphens in command names, non-empty `Short` and `Long` (`internal/cmd/conventions_test.go`).
- A doc page at `docs/cmd/<domain>-<verb>.md` for each leaf command, and a README link to it.
- No import from `internal/cmd/<x>` into `internal/cmd/<y>`.
- `fmt.Print*`, `os.Stdout`, `os.Stderr` and `os.Getenv` outside the allowed paths (forbidigo in
  `.golangci.yml`).

Check these (lint) rules by hand. They have no lint today:

- Credentials come only through `App` accessors (`CredentialStore`, `APIKeyResolver`,
  `TokenResolver`, `ControlPlaneToken`, `DataPlane`, `ControlPlane`). Command code does not read the keychain, a credential file or a
  token env var directly.
- Config reads go through `a.Config`. Only `internal/config` reads a config file.
- A command that needs the control plane calls `a.ControlPlane()`. A command that needs the data
  plane calls `a.DataPlane()`. A mismatch is Important.
- Errors go to stderr with a non-zero exit, also with `--output json`. A command does not print an
  error and then return nil.
- Each package under `internal/cmd/` has at least one `*_test.go`.
- One package per domain. No parallel `internal/domain/<x>` tree.

## What Important means here

Report a finding as Important when the change does one of these:

- Prints, logs or puts in an error message a token, an API key, a refresh token or a keychain
  value. The one exception is the access token that `safedep auth token` writes to stdout.
- Breaks a (lint) rule from the list above.
- Swallows an error: `_ =` on a call that returns an error, an empty `if err != nil {}`, or a
  `log.Warnf` where the caller needs the failure.
- Changes stdout data that scripts read: the JSON shape of `RenderJSON`, or exact values in
  `RenderPlain`.
- Edits a released migration under `internal/storage/migrations/sqlite/`. Migrations are append-only.
- Stores per-profile state without the profile in the key. Use `app.ProfileKV[T]`.
- Adds a GitHub Action in `.github/workflows/` with a tag or branch ref, not a full commit sha.
- Adds business logic to `RunE` or to `App`. `RunE` resolves dependencies, calls the
  orchestration and prints.

Style, names and refactors are Nit at most.

## Always check

- `RenderTable` humanizes times and durations only with `dry/tui/humanize` (`Time`, `Duration`).
  A local time or duration formatter in a command package is a finding. `RenderPlain` and
  `RenderJSON` keep exact values (RFC 3339 instants, raw durations).
- Messages use `dry/tui` (`Info`, `Success`, `Warning`, `Error`). Data commands implement
  `tui.Renderable` and print with `a.Output.Print`. Do not reimplement a table, spinner or banner.
- Commands do not call `storage.Open` and do not write SQL. They use the `App` storage accessors.
- Upstream tools (vet, pmg, gryph) run as subprocesses. Upstream types stop at the boundary.
- An interface with two or more methods gets a mockery v3 mock: an entry in `.mockery.yml`, output
  in `mocks/`, regenerated with `go tool mockery`. A hand-written fake is fine for a single-method
  interface or a function type.
- Tests use `testify`: `require` for fatal checks, `assert` for the rest. Prefer table-driven
  tests. Service tests use fakes, with no real network or database.
- Internal logs use `dry/log`.
- Code comments are ASCII only. No em-dash, no emoji, no `;` to join two sentences. A comment says
  why, not how Go works (`defer`, `context`, `sync.Mutex`). Error messages use `label: detail`.
- A new verb in `internal/cmd/verbs.go` has a one-line reason in the PR description.
- A new leaf command at depth 1 needs a written reason in `rootLevelExceptions`. The default
  answer is no.

## Evidence

A claim about behavior needs a `file:line` citation in the source. Do not infer behavior from a
name.

## Cap the nits

Report at most five Nits per review. Put Important findings first.

## Do not report

- Anything that `make lint`, `make lint-conventions`, `make test` or CodeQL already reports:
  golangci-lint (errcheck, govet, staticcheck, gosec, errorlint, gocritic, revive, misspell,
  forbidigo) and the convention tests.
- Secrets that gitleaks finds in the pre-commit hook.
- Generated mocks in `**/mocks/*_mock.go`. Review `.mockery.yml` instead.
- Lockfiles and module sums: `pnpm-lock.yaml`, `go.sum`, `go.work.sum`.
- Version numbers in `packages/*/package.json`. `scripts/sync-binaries` sets them at release.
