# AGENTS.md

Guidance for AI coding agents. Sources of truth:

- [docs/ADR.md](./docs/ADR.md) — architectural decisions and rationale.
- [docs/DEVGUIDE.md](./docs/DEVGUIDE.md) — operational rules, layout, lints, walkthrough.

Coding agents and code review agents must read and conform to both. Treat any rule tagged **(lint)** in DEVGUIDE as required even when CI hasn't caught up.

## Build

```bash
make deps             # Download dependencies
make build            # Build binary → bin/safedep
make test             # Run tests
make lint             # Run linter (golangci-lint)
make lint-conventions # Run lint + convention tests over the cobra tree
make release-snapshot # Local release build via goreleaser
```

## Guidance

- NO EMOJI
- Refactor code when required instead of violating DRY
- Keep this file distilled. Avoid fluff
- No unnecessary comments
- Code comments must be ASCII only. No em-dash, unnecessary compound words
- Do not use `;` to join sentences. In error messages use `:` (label:detail). In comments end the sentence with `.` and start the next one
- Idiomatic Go: explicit error handling, table-driven tests, no swallowed errors
- Use `testify/require` for fatal assertions, `testify/assert` for non-fatal
- Generate mocks with mockery v3 (testify template) for non-trivial interfaces. Hand-rolled fakes for single-method interfaces or function-type parameters. See DEVGUIDE Mocks section
- Re-use existing patterns. Prefer refactoring over copying
- `dry/log` for internal logging (`log.Warnf` for soft failures)

For any changes in `.github/workflows/`:

- Always pin GitHub Actions to commit sha
- Always check latest stable version of a GitHub action before use
