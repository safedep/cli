# Developer guide

Operational rules and walkthrough for contributors and AI agents. Decisions and rationale live in [ADR](./ADR.md).

CI enforces every rule marked **(lint)**. If a rule is not yet enforced, treat it as required during review until the lint catches up.

## Layout

```
cmd/safedep/                  entry point; wires App and root cobra command
internal/
  app/                        App DI container; owns credentials and plane clients
  config/                     config load + override precedence
  tui/                        Renderable contract, Printer dispatcher, CLI theme
  version/                    build metadata
  cmd/                        verb allow-list lives at cmd/verbs.go
    <domain>/                 one package per top-level noun
      cmd.go                  Register(root, app) + parent cobra command
      <verb>.go               leaf command: cobra wiring + types + orchestration + render
      service.go              optional; only when multiple verbs share orchestration
docs/
  ADR.md                      architectural decisions
  DEVGUIDE.md                 this file
  cmd/<domain>-<verb>.md      one per leaf command
```

Deferred subsystems (introduced when first command needs them):

- `internal/auth/` — currently App methods are the auth surface; a dedicated package will land when the credential surface grows beyond accessors.
- `internal/orchestrator/` — subprocess adapter for upstream tools; deferred until the first integration.
- `internal/testutil/` — fakes and golden helpers; added with the first reusable test fixture.
- `pkg/` — CLI public API; stays empty until an external consumer exists.

Logging uses `dry/log` directly; the CLI does not wrap it.

- One package per domain. No parallel `internal/domain/<x>` tree. **(lint)**
- Operational messaging (Info/Success/Warning/Error, Table, Banner, Spinner, etc.) comes from [`dry/tui`](https://github.com/safedep/dry/tree/main/tui). Commands import it directly. The CLI does not wrap these primitives.
- `internal/tui/` owns the CLI's data-presentation contract (`Renderable`), the dispatcher (`Printer`), and the theme that gets pushed into dry/tui at startup. It is not a wrapper around dry/tui's messaging helpers.
- Cross-tool reusable code goes to `github.com/safedep/dry`, not `internal/`.

## Command shape

- Every leaf command path is `safedep <noun> [<noun>...] <verb>`. Minimum depth 2. **(lint)**
- The leaf segment's first token must be in the verb allow-list at `internal/cmd/verbs.go`. **(lint)**
- Adding a verb requires updating `verbs.go` with a one-line justification in the PR description.
- No hyphens in command names (`run-scan` is rejected; use `scan run`). **(lint)**
- `cobra.Command.Short` and `Long` must be non-empty. **(lint)**

Initial verb allow-list:
`get, list, show, run, exec, login, logout, status, install, uninstall, enable, disable, create, delete, update, set, init, sync, edit`.

### Exceptions

A small number of commands are intentionally allowed at depth 1 against the noun-verb rule, where universal CLI convention is older and stronger than ours:

- `safedep version` — every CLI in the ecosystem (kubectl, gh, git, vet, pmg) responds to a bare `version`. Forcing `version show` would cost UX without buying anything.

Exceptions live in `rootLevelExceptions` in `internal/cmd/conventions_test.go`. Adding to the list requires a PR explaining why the universal convention overrides ours. Default answer: no.

## Naming

- Service structs use `-er` when the verb forms a natural noun (`Runner`, `Scanner`, `Installer`). Use `Service` suffix when it does not (`SetupService`, `QueryService`).
- Never `Manager` or `Handler`.
- Per-command I/O types live next to the command and use lowercase names (`runInput`, `runResult`); they are not part of any public API.

## Output

Two distinct concerns:

**Messaging** (Info / Success / Warning / Error). For both humans and AI agents. Commands call `dry/tui.Info / Success / Warning / Error` directly. dry/tui auto-detects its mode (rich, plain, agent) from TTY state, env vars (`CLAUDE_CODE`, `ANTHROPIC_AGENT`, `CI`, `TERM=dumb`), and `SAFEDEP_OUTPUT`. Human users get colour and unicode; agents get terse, token-optimised text. The `--output` flag does **not** influence messaging.

**Renderable** (data presentation). Data commands implement `tui.Renderable`:

```go
type Renderable interface {
    RenderJSON() ([]byte, error)
    RenderTable() string
    RenderPlain() string
}
```

`a.Output.Print(r)` writes the active mode's representation to stdout. The `--output` flag selects the mode:

- `--output table` (the rich variant): humans, decorated.
- `--output plain`: humans on basic terminals or shell pipelines.
- `--output json`: programmatic and agent consumers.
- `--output` empty: auto-detect via dry/tui (rich -> table, plain -> plain, agent -> json).

Lists implement the same interface. `RenderTable` produces a table, `RenderPlain` emits a line per item, `RenderJSON` returns the encoded array. Pre-built helpers will be extracted once a second list-shaped command lands.

Operational commands (no structured result) call `dry/tui` directly and do not implement `Renderable`.

- Errors always go to stderr with non-zero exit, regardless of `--output`. **(lint)**
- No direct use of `fmt.Println`, `fmt.Printf`, `os.Stdout`, or `os.Stderr` outside `internal/tui` and `dry/tui` call sites in commands. **(lint, depguard)** The single allowed exception is the top-level error path in `cmd/safedep/main.go`, which writes the fatal error to stderr before exit.
- Reusable visual components (tables, banners, diffs, badges, spinners, progress) come from `dry/tui` sub-packages. Do not reimplement.

## Authentication

- API key (data plane): `a.DataPlane()`.
- JWT (control plane): `a.ControlPlane()`.
- Both return user-facing errors. Propagate them directly.
- A static map of command → required plane lives in the convention test; mismatches fail CI. **(lint)**
- Credentials are accessed only through `App` accessors (`CredentialStore`, `APIKeyResolver`, `TokenResolver`, `DataPlane`, `ControlPlane`). No direct keychain or env-var reads from command code. **(lint, depguard)** A dedicated `internal/auth` package will absorb these accessors when the credential surface grows.
- The `--insecure-keychain-fallback` persistent flag opts the keychain into a plaintext-file backend when no OS keychain is available. Off by default. Wired through `App.SetInsecureKeychainFallback` so every `App` accessor that builds a store or resolver picks up the same setting.

### Profiles

- The `--profile` flag is a persistent flag on the root command; all subcommands inherit it.
- `App` resolves the active profile once at init from `--profile`, `SAFEDEP_PROFILE`, persisted default, then `"default"`.
- Command code never references the active profile directly. `a.DataPlane()` and `a.ControlPlane()` build clients with the resolved profile via `dry/cloud`'s `WithProfile`.
- The `auth` domain owns profile-management verbs: `login`, `logout`, `status`, `profile list`. Profile creation and deletion happen nowhere else.
- Local state cached per profile (last-used tenant, command-specific caches) must be keyed by profile name. Global preferences (output format, etc.) are unscoped.

## Configuration

- All config reads go through `a.Config`. The package applies the override precedence from ADR.
- No direct env var or config file reads outside `internal/config`. **(lint, depguard)**

## Storage

Local CLI state lives in sqlite under `internal/storage`. A single
`storage.Storage` is exposed via `App.Storage()` (lazy, process-scoped,
closed in `App.Close`).

- Commands never call `storage.Open` and never write SQL. They obtain
  primitives via `App` accessors.
- The `KV[T]` primitive is the default for command-specific state.
  See [storage-kv.md](./storage-kv.md) for the full guide and call-site
  examples. Per-profile state uses `app.ProfileKV[T]`; unscoped state
  uses `app.GlobalKV[T]`.
- Schema changes are forward-only via embedded migrations under
  `internal/storage/migrations/sqlite/`. Files are append-only once
  released; a downgrade refuses to open with `ErrSchemaTooNew`.
- Cross-cutting operations (`Stats`, `Cleanup`) are descriptor-driven so
  adding a new primitive is a one-line append in `descriptor.go` plus
  the primitive's own file. Future `safedep doctor` and `safedep
  cleanup` commands consume these.
- Daemon-mode commands needing Postgres or MySQL backends will land as
  sibling implementations of `storage.Storage`; callers do not change.
- `dry/endpointsync` is a use-case-specific WAL inside DRY. Do not
  generalise it as CLI storage.

## External tool orchestration

- Upstream SafeDep tools (vet, pmg, gryph) are invoked as subprocesses, not linked.
- The orchestrator interface lives at `internal/orchestrator/` and is added when the first command needs it.
- Commands depend on the orchestrator interface, not on upstream binaries directly.
- Translate upstream output types into CLI-side structs at the boundary so upstream changes do not leak into command I/O.

## Documentation

- Every leaf command has a doc page at `docs/cmd/<domain>-<verb>.md`. **(lint)**
- The README must contain a link to every doc page. **(lint)**
- Doc pages cover: synopsis, flags, common use cases, exit codes. Keep them concise.

## Code health

- `RunE` is wiring only: resolve deps, call orchestration, print. No business logic.
- Per-leaf-command file size soft cap: 200 lines. Beyond that, extract `service.go`.
- Every command package has at least one `*_test.go`. **(lint)**
- Service tests use fakes of the package's own interfaces. No real network or DB in `service_test.go`.
- No package under `internal/cmd/<x>` may import `internal/cmd/<y>` for `x != y`. **(lint)**
- Idiomatic Go: explicit error handling, table-driven tests, no swallowed errors, `dry/log` for internal logs.
- Use `testify/require` for fatal assertions, `testify/assert` for non-fatal.

## CI

`make lint-conventions` runs all linted rules. It must pass before merge.

The lint targets are added in Phase 2. Rules tagged **(lint)** are currently enforced by code review only; treat them as required.

---

# Adding a domain

A *domain* is the top-level noun in a `safedep` command. This walkthrough shows how to add one.

## Anatomy of a leaf command

`RunE` does exactly three things, in order:

1. Resolve dependencies from `a` (App) and flag values.
2. Call the orchestration (a function in this file, or a service struct).
3. Print the result via `a.Output.Print(result)` for data commands, or via `tui.Info / Success / Warning` for operational ones.

If `RunE` does anything else, the code belongs in a function or a `service.go`.

```go
// internal/cmd/scan/run.go
package scan

import (
    "github.com/safedep/cli/internal/app"
    "github.com/spf13/cobra"
)

type runInput struct {
    ManifestPath string
    Ecosystem    string
}

type runResult struct {
    Packages []packageResult `json:"packages"`
    Total    int             `json:"total"`
}

func (r *runResult) RenderJSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }
func (r *runResult) RenderTable() string         { ... }
func (r *runResult) RenderPlain() string         { ... }

func runCmd(a *app.App) *cobra.Command {
    var in runInput

    cmd := &cobra.Command{
        Use:   "run",
        Short: "Scan a manifest for vulnerabilities",
        RunE: func(cmd *cobra.Command, args []string) error {
            client, err := a.DataPlane()
            if err != nil {
                return err
            }

            result, err := runScan(cmd.Context(), client, in)
            if err != nil {
                return err
            }
            return a.Output.Print(result)
        },
    }

    cmd.Flags().StringVarP(&in.ManifestPath, "manifest", "f", "", "path to manifest file")
    cmd.Flags().StringVar(&in.Ecosystem, "ecosystem", "", "ecosystem (npm, pypi, ...)")
    return cmd
}
```

Types live next to the command. They are this command's I/O, not a shared domain model. Translate upstream types at the boundary so upstream libraries stay swappable.

## When to extract a service

Extract a `service.go` only when:

- Two or more verbs in the same domain share non-trivial orchestration, OR
- The orchestration deserves unit tests in isolation from cobra.

Otherwise keep the orchestration as a function in the verb file.

## Injecting collaborators

When orchestration depends on something that is hard to fake in a unit test (network, filesystem, subprocess, interactive prompt), accept it as an interface or function-type parameter rather than reaching out from inside the orchestration. The cobra wiring passes the real implementation. The test passes a fake.

```go
// internal/cmd/scan/service.go
type packageRepo interface {
    ListVulnerabilities(ctx context.Context, pkg string) ([]vuln, error)
}

func runScan(ctx context.Context, repo packageRepo, in runInput) (*runResult, error) {
    // pure orchestration, no network calls of its own
}
```

Concrete implementations (the gRPC client wrapper, the file reader, etc.) live in the same package as a sibling file, e.g. `grpc_repo.go`. Pull them out only when reused across packages, in which case they belong somewhere shared (`dry`, or a future `internal/...` package).

## Mocks

Use [mockery v3](https://vektra.github.io/mockery/) to generate mocks for non-trivial interfaces, matching the convention used in `malysis`. Hand-rolled struct fakes are fine for single-method interfaces or function-type parameters (such as the `TenantPicker` in `internal/auth/bootstrap.go`). Reach for mockery when an interface has two or more methods, or when tests need to verify call sequences or argument capture.

Conventions:

- `.mockery.yml` at repo root lists packages and interfaces explicitly. No `//go:generate` directives in source files.
- Generated mocks land in a `mocks/` subdir under the interface's package, with type name `<Interface>Mock` and file name `<Interface>_mock.go`.
- Generated files are committed to the repo so test runs are hermetic.
- Use the testify template (`template: testify`) so mocks integrate with `testify/assert` and `testify/require` already used elsewhere.
- mockery is wired as a Go tool dependency (`tool ( github.com/vektra/mockery/v3 )` in `go.mod`). Regenerate with `go tool mockery`.

`.mockery.yml` and the `tool` directive are added when the first interface needs mocking. The first PR doing so should also wire a `make mocks` target.

## App accessors

`App` is a DI container. Do not add business logic to it.

| Need | Use |
|------|-----|
| Data plane gRPC client | `a.DataPlane()` |
| Control plane client (Phase 2) | `a.ControlPlane()` |
| Credential store | `a.CredentialStore()` |
| Credential resolver | `a.CredentialResolver()` |
| Active credential profile | `a.Profile()` (read-only; flag/env wired in root) |
| Config | `a.Config` |
| Output dispatcher | `a.Output` |
| Storage (sqlite, raw access) | `a.Storage()` |
| Typed KV (per profile) | `app.ProfileKV[T](a, "<namespace>")` |
| Typed KV (global) | `app.GlobalKV[T](a, "<namespace>")` |

## Registration

```go
// internal/cmd/scan/cmd.go
package scan

import (
    "github.com/safedep/cli/internal/app"
    "github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
    parent := &cobra.Command{
        Use:   "scan",
        Short: "Scan dependencies for vulnerabilities",
    }
    parent.AddCommand(runCmd(a))
    root.AddCommand(parent)
}
```

One line in `cmd/safedep/main.go`:

```go
scan.Register(root, a)
```

## Checklist

Adding domain `foo` with a `bar` command:

- [ ] `internal/cmd/foo/cmd.go`: `Register(root, a)` and parent cobra command
- [ ] `internal/cmd/foo/bar.go`: cobra command, types, orchestration, Renderer (if data)
- [ ] `internal/cmd/foo/bar_test.go`
- [ ] `cmd/safedep/main.go`: `foo.Register(root, a)`
- [ ] Verb `bar` is in `internal/cmd/verbs.go` (add with justification if new)
- [ ] `docs/cmd/foo-bar.md` written
- [ ] README index updated
- [ ] `RunE` is wiring only; no business logic
- [ ] `make lint-conventions` passes

## Examples

| Domain | Verbs | Cmd |
|--------|-------|-----|
| `protect/mcp` | `status`, `install`, `uninstall` | `internal/cmd/protect/mcp/` |
| `doctor` | `run` | `internal/cmd/doctor/` |
| `auth` | `login`, `logout`, `status` | `internal/cmd/auth/` |
