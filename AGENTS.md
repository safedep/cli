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
- Do not explain how Go works (e.g. how `context.Context` propagates cancellation, what a `defer` does, what `sync.Mutex` guards). Comment the *why* and the non-obvious invariants only
- Code comments must be ASCII only. No em-dash, unnecessary compound words
- Do not use `;` to join sentences. In error messages use `:` (label:detail). In comments end the sentence with `.` and start the next one
- Idiomatic Go: explicit error handling, table-driven tests, no swallowed errors
- Use `testify/require` for fatal assertions, `testify/assert` for non-fatal
- Generate mocks with mockery v3 (testify template) for non-trivial interfaces. Hand-rolled fakes for single-method interfaces or function-type parameters. See DEVGUIDE Mocks section
- Re-use existing patterns. Prefer refactoring over copying
- `dry/log` for internal logging (`log.Warnf` for soft failures)
- Humanize table cells with `dry/tui/humanize` (`Time`, `Duration`). Never a local formatter. New value shapes extend the dry package. See DEVGUIDE Output section
- Comments explain the current behaviour and its reason. No past-facing wording such as "keep the old behaviour". State the actual constraint instead
- DO NOT leak our conversation, prompt, or iteration history into code comments, pull request descriptions, or other maintainer-facing prose. Write for readers who have not seen our conversation

## Engineering principles

- Reuse before you build: the Go standard library, this repository, then modules already in `go.mod` (`safedep/dry`, cobra, testify). Grep for a helper before you write one
- New dependencies must be well known and maintained. Propose one before adding it when it overlaps an existing one. Bump one module with `go get <module>@<version>`, never `go get -u ./...`
- SOLID, DRY by the rule of three, KISS, YAGNI. No options, wrappers or config for a single caller
- Prefer guard clauses and early returns. Use the same name for the same concept everywhere
- Export the smallest surface. Define an interface where it is consumed, with only the methods that caller uses

## Tests

- Every test guards a behaviour a regression could break, or a bug that already happened. No smoke tests, no tests that restate the implementation, no tests that only raise coverage
- Test through the entry point a caller uses (the cobra command, the orchestration function) and assert outcomes, not the calls made inside
- Fake a dependency at its port: a small fake of the interface, or an `httptest` server for a remote API. A mock that returns fixed values for every call proves nothing
- A new test must fail when you remove the code it guards. Check that once before you commit
- Before adding a test, check that no existing test covers the behaviour. Extend an existing test before creating a new one
- For a parser or a mapping over free-form input, add a native `Fuzz` test with seeds. CI replays the seeds. Run `go test -fuzz` locally for a while before you commit
- Do not assume a failure is pre-existing. Reproduce it on `main` before calling it flaky, and file an issue

For any changes in `.github/workflows/`:

- Always pin GitHub Actions to commit sha
- Always check latest stable version of a GitHub action before use
