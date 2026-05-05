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

## CI / GitHub Actions

Always pin third-party GitHub Actions to a full commit SHA, not a tag or branch. A tag can be moved silently by the action's owner; a SHA cannot. This is the project's only defence against supply-chain compromise of upstream actions.

Format:

```yaml
# actions/checkout v6.0.2
- uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd
```

Always include a comment with the human-readable version on the line immediately above so the SHA stays auditable. When bumping, resolve the new SHA via `gh api repos/<owner>/<repo>/git/refs/tags/<version>` and update both the SHA and the comment in the same commit.

This rule applies to every action under `.github/workflows/` regardless of trust. First-party actions from `actions/*` are not exempt.
