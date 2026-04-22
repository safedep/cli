# Adding a new domain

This guide walks through how to add a new command domain to the CLI. It is grounded in the architecture we use — not a theoretical model.

## The three layers

```
internal/domain/<domain>/     ← business logic + output types
internal/protect/... (infra)  ← external systems (adapters, APIs, file I/O)
internal/cmd/<domain>/        ← cobra commands, thin wiring only
```

The dependency arrow goes one way: **cmd → domain → infra**. Domain packages never import `internal/cmd`. Cmd packages never contain business logic.

### What goes where

| Layer | Owns | Does not own |
|-------|------|--------------|
| `domain` | service types, input/output structs, Renderable impls | cobra, App, output printing |
| `infra` | adapter interfaces, external API clients, file I/O | command flags, output formatting |
| `cmd` | cobra command definitions, flag parsing, output printing | any logic beyond wiring |

---

## Anatomy of a domain service

A domain service is a struct with an action method. Inputs and outputs are typed structs; the output implements `output.Renderable` when it is data (not operational messages).

```go
// internal/domain/scan/runner.go
package scan

type RunInput struct {
    ManifestPath string
    Ecosystem    string
}

type RunResult struct {
    Packages []PackageResult `json:"packages"`
    Total    int             `json:"total"`
}

// RenderJSON / RenderTable / RenderPlain make RunResult printable via -o flag.
func (r *RunResult) RenderJSON() ([]byte, error)  { ... }
func (r *RunResult) RenderTable() string           { ... }
func (r *RunResult) RenderPlain() string           { ... }

type Runner struct {
    // infrastructure dependencies go here as interfaces
    Repo PackageRepository
}

func (r *Runner) Run(ctx context.Context, in RunInput) (*RunResult, error) {
    // pure business logic; no cobra, no App, no output printing
}
```

**Naming:**
- Use the `-er` suffix when the verb forms a natural noun: `Runner`, `Scanner`, `Installer`, `Checker`, `Viewer`.
- Use `Service` suffix when it doesn't: `SetupService`, `QueryService`.
- Never use `Manager` or `Handler` — too vague.

**When to implement Renderable:**
- Data commands (`scan run`, `query run`, `auth status`): yes — the result is structured data that benefits from `-o json` / `-o table` / `-o plain`.
- Operational commands (`mcp install`, `auth login`): no — output is a series of progress messages; the cmd layer prints them using `a.Output.Info/Success/Warning`.

---

## Infrastructure interfaces (repositories)

When the domain service depends on an external system, define an interface in the domain package (at the point of use) and implement it in `internal/cmd/<domain>/` or a dedicated infra package.

```go
// internal/domain/scan/repository.go
package scan

import "context"

// PackageRepository is the only thing Runner knows about the outside world.
type PackageRepository interface {
    ListVulnerabilities(ctx context.Context, pkg string) ([]Vulnerability, error)
}
```

The cmd layer provides a concrete implementation:

```go
// internal/cmd/scan/repository.go
package scan

import (
    "context"
    scandomain "github.com/safedep/cli/internal/domain/scan"
    "github.com/safedep/dry/cloud"
)

type grpcPackageRepository struct {
    client *cloud.Client
}

func (r *grpcPackageRepository) ListVulnerabilities(ctx context.Context, pkg string) ([]scandomain.Vulnerability, error) {
    // call the gRPC API
}
```

---

## The cmd layer

Cmd files are thin. A command's `RunE` does exactly three things:

1. Resolve dependencies from `a` (App) or flags.
2. Call the domain service.
3. Print the result or handle output.

```go
// internal/cmd/scan/run.go
package scan

import (
    "github.com/safedep/cli/internal/app"
    scandomain "github.com/safedep/cli/internal/domain/scan"
    "github.com/spf13/cobra"
)

func runCmd(a *app.App) *cobra.Command {
    var manifest, ecosystem string

    cmd := &cobra.Command{
        Use:   "run",
        Short: "Scan a manifest for vulnerabilities",
        RunE: func(cmd *cobra.Command, args []string) error {
            client, err := a.RequireDataPlane()
            if err != nil {
                return err
            }

            runner := &scandomain.Runner{
                Repo: &grpcPackageRepository{client: client},
            }

            result, err := runner.Run(cmd.Context(), scandomain.RunInput{
                ManifestPath: manifest,
                Ecosystem:    ecosystem,
            })
            if err != nil {
                return err
            }

            return a.Output.Print(result)
        },
    }

    cmd.Flags().StringVarP(&manifest, "manifest", "f", "", "path to manifest file")
    cmd.Flags().StringVar(&ecosystem, "ecosystem", "", "ecosystem (npm, pypi, ...)")
    return cmd
}
```

---

## App is a DI container, not a service

`App` holds infrastructure components. Access them via its methods; do not add business logic to it.

| Need | Use |
|------|-----|
| Data plane gRPC client | `a.RequireDataPlane()` |
| Control plane client (Phase 2) | `a.RequireControlPlane()` |
| Credential store | `a.CredentialStore()` |
| Credential resolver | `a.CredentialResolver()` |
| Config | `a.Config` |
| Output formatter | `a.Output` |

`RequireDataPlane` and `RequireControlPlane` return clear errors when prerequisites are not met. Always propagate them directly — the error message is user-facing.

---

## Registration

Every domain has a `Register` function that wires commands into the root:

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

Then one line in `cmd/safedep/main.go`:

```go
scan.Register(root, a)
```

---

## Complete checklist

When adding a new domain `foo` with a `bar` command:

- [ ] `internal/domain/foo/bar.go` — `BarInput`, `BarResult` (+ Renderable if data), `Barer` service
- [ ] `internal/domain/foo/repository.go` — infra interfaces, if needed
- [ ] `internal/cmd/foo/bar.go` — cobra command, wires domain service
- [ ] `internal/cmd/foo/repository.go` — concrete infra implementations, if needed
- [ ] `internal/cmd/foo/cmd.go` — `Register(root, a)`
- [ ] `cmd/safedep/main.go` — `foo.Register(root, a)`
- [ ] Domain service has no import of `internal/cmd`, `internal/app`, or `github.com/spf13/cobra`
- [ ] `RunE` contains no business logic beyond flag wiring + calling the domain service
- [ ] `BarResult` implements `RenderJSON`, `RenderTable`, `RenderPlain` if it is data output

---

## Real examples in this codebase

| Domain | Service | Input/Result | Cmd |
|--------|---------|--------------|-----|
| `domain/protect/mcp` | `StatusChecker` | `StatusResult` (Renderable) | `cmd/protect/mcp/status.go` |
| `domain/protect/mcp` | `Provisioner` | `ProvisionResult`, `DeprovisionResult` | `cmd/protect/mcp/install.go`, `uninstall.go` |
| `domain/doctor` | `Checker` | `CheckResult` (Renderable) | `cmd/doctor/cmd.go` |
| `domain/auth` | `Saver` | — | `cmd/auth/login.go`, `cmd/setup/mcp.go` |
