# SafeDep CLI

## Build

```bash
just deps             # Download dependencies
just build            # Build binary → bin/safedep
just test             # Run tests
just lint             # Run linter (golangci-lint)
just release-snapshot # Local release build via goreleaser
```

## Architecture

```
cmd/safedep/        → Entry point; wires App, calls root.Execute()
internal/app/       → App struct (dependency container injected into all commands)
internal/config/    → TOML config: ~/.config/safedep/config.toml (non-secret state)
internal/output/    → Formatter: -o flag dispatch, Renderable interface
internal/protect/   → MCP adapter interface + IDE implementations
internal/cmd/       → Cobra commands, one package per domain
```

## Command pattern

Each domain is its own package under `internal/cmd/<domain>/` with a `Register(root *cobra.Command, app *app.App)` function. Adding a new command = new file, one `Register` call in `main.go`.

## Auth model

- Phase 1: API key only. `app.DataPlane` is the only wired client.
- `app.ControlPlane` is nil until OAuth lands (Phase 2). Commands needing it must call `app.RequireControlPlane()` and return its error with a clear message.
- Credentials stored via `dry/cloud.CredentialStore` → OS keychain with file fallback for WSL2/headless.

## Output

- `-o` / `--output` flag: `table` (default), `plain`, `json`
- Data commands implement `output.Renderable`. Operational commands use `tui.Info/Success/Error`.
- Errors always go to stderr + non-zero exit, regardless of `-o`.

## Code style

- No unnecessary comments
- Idiomatic Go: explicit error handling, table-driven tests, no swallowed errors
- Use `testify/require` for fatal assertions, `testify/assert` for non-fatal
- Re-use existing patterns; prefer refactoring over copying
- `dry/log` for internal logging (`log.Warnf` for soft failures)
