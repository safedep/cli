# NPM + Go Binary Packaging

**Date:** 2026-05-19
**Goal:** `npx @safedep/cli setup mcp` — zero-install first-run onboarding for developers

---

## Scope

Ship the build + sync + JS shim pipeline. Release/publish automation, macOS notarization, and additional platforms are explicitly deferred.

---

## Repo Structure

```text
safedep-cli/
├── go.mod                          # unchanged
├── go.work                         # NEW: use . / use ./scripts
├── nx.json                         # NEW
├── pnpm-workspace.yaml             # NEW
├── package.json                    # NEW: private root
├── pnpm-lock.yaml                  # generated
├── packages/
│   ├── cli/                        # @safedep/cli — ESM shim
│   │   ├── src/bin.ts
│   │   ├── tsdown.config.ts
│   │   ├── tsconfig.json
│   │   └── package.json
│   ├── cli-linux-x64/              # @safedep/cli-linux-x64
│   │   ├── bin/                    # .gitignored — populated by sync-binaries
│   │   └── package.json
│   ├── cli-linux-arm64/
│   ├── cli-darwin-x64/
│   ├── cli-darwin-arm64/
│   ├── cli-win32-x64/
│   └── smoke/                      # private tarball smoke test
│       └── package.json
└── scripts/
    ├── go.mod                      # NEW: shared scripts module
    ├── go.sum
    ├── sync-binaries/main.go       # port from PoC
    └── check-version-sync/main.go  # port from PoC
```

`packages/*/bin/` is in `.gitignore`. Binaries are build outputs only.

---

## Go Workspace

`go.work` at repo root:

```text
go 1.26.2
use .
use ./scripts
```

`scripts/go.mod` module path: `github.com/safedep/cli/scripts`. One shared module for both scripts. `GOWORK` is not overridden in goreleaser — no `replace` directives exist, and goreleaser v2 handles `go.work` natively.

---

## GoReleaser Changes

Extend `.goreleaser.yaml` platform matrix only:

```yaml
builds:
  - env:
      - CGO_ENABLED=0
    binary: safedep
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ignore:
      - goos: windows
        goarch: arm64
    main: ./cmd/safedep/
```

No other changes. `dist/artifacts.json` is produced by goreleaser and consumed by `sync-binaries`.

---

## npm Package Structures

### Meta package: `@safedep/cli`

```json
{
  "name": "@safedep/cli",
  "version": "0.0.0",
  "type": "module",
  "bin": { "safedep": "dist/bin.mjs" },
  "files": ["dist/**"],
  "optionalDependencies": {
    "@safedep/cli-linux-x64":    "workspace:*",
    "@safedep/cli-linux-arm64":  "workspace:*",
    "@safedep/cli-darwin-x64":   "workspace:*",
    "@safedep/cli-darwin-arm64": "workspace:*",
    "@safedep/cli-win32-x64":    "workspace:*"
  }
}
```

Built by `tsdown`. Ships only `dist/bin.mjs`.

### Platform packages (example: `@safedep/cli-linux-arm64`)

```json
{
  "name": "@safedep/cli-linux-arm64",
  "version": "0.0.0",
  "os": ["linux"],
  "cpu": ["arm64"],
  "files": ["bin/**"],
  "bin": { "safedep-cli": "bin/safedep" }
}
```

The dummy `bin` entry (`safedep-cli`) forces npm/pnpm to preserve the executable bit on pack/install. All 5 platform packages follow this pattern; `win32` uses `bin/safedep.exe` and `"safedep-cli": "bin/safedep.exe"`.

### Smoke package

```json
{
  "name": "smoke",
  "private": true,
  "dependencies": {
    "@safedep/cli": "workspace:*"
  }
}
```

Used only for local tarball validation (see Nx targets below).

### ESM shim (`packages/cli/src/bin.ts`)

- Detects `process.platform` + `process.arch`
- Resolves `@safedep/cli-${platform}-${arch}/package.json` via `require.resolve`
- Spawns `bin/safedep[.exe]` with `process.argv.slice(2)` and `stdio: "inherit"`
- Propagates exit code and signal faithfully
- Emits a structured error message listing common failure causes (missing optional dep, unsupported platform, missing binary in package)

---

## sync-binaries

Reads `dist/artifacts.json`, maps GOOS/GOARCH to Node platform naming, copies binaries to `packages/cli-<os>-<arch>/bin/`.

Key invariants:

- Keeps `github.com/go-playground/validator/v10` for struct validation of artifact entries.
- Always runs with `--strict=true`: fails if a goreleaser target has no matching `packages/` directory. This is the safety gate for adding a new platform to goreleaser without creating the corresponding npm package.
- Preserves source file permissions on copy (`os.Chmod` with source mode).
- GOOS/GOARCH mapping: `windows → win32`, `amd64 → x64`. `darwin` and `linux` pass through unchanged. `arm64` passes through unchanged.

---

## check-version-sync

Reads all non-private `packages/*/package.json` and asserts all versions are identical.

- During development: run without `--require-tag` (version agreement only).
- At release (deferred): run with `--require-tag=true` to assert all npm versions match the git tag on HEAD.

---

## Nx Orchestration

### Dependency chain

```text
repo:build-dev
  └── @safedep/cli:build          (tsdown)
        └── sync-binaries:run      (go run ./scripts/sync-binaries/ --strict)
              └── safedep-go:build-snapshot  (goreleaser build --clean --snapshot)

repo:verify
  └── smoke:verify
        └── repo:build-dev
```

### Projects

| Project | Root | Target | Command |
| --- | --- | --- | --- |
| `safedep-go` | `.` | `build-snapshot` | `goreleaser build --clean --snapshot` |
| `sync-binaries` | `scripts/sync-binaries` | `run` | `go run ./scripts/sync-binaries/ --strict --artifacts-path dist/artifacts.json --packages-path ./packages` |
| `@safedep/cli` | `packages/cli` | `build` | `tsdown` |
| `smoke` | `packages/smoke` | `verify` | `pnpm pack -r --pack-destination ./smoke-tarballs && pnpm install --no-frozen-lockfile && pnpm exec safedep --version` |
| `repo` | `.` | `build-dev` | orchestrator — chains all above |

### Nx cache inputs

- `safedep-go`: `**/*.go`, `go.mod`, `go.sum`
- `@safedep/cli`: `packages/cli/src/**`, `packages/cli/tsdown.config.ts`

### Developer workflow

```bash
pnpm nx run repo:build-dev    # full build
pnpm nx run repo:verify       # build + smoke test
pnpm nx run @safedep/cli:build  # JS shim only (cached if Go unchanged)
```

---

## Version Invariant

All `packages/*/package.json` (non-private) must share the same version at all times. Seeded at `0.0.0`. Bumped manually via `pnpm nx release version` before the first publish. `check-version-sync` enforces this in CI (without `--require-tag` until publish automation lands).

---

## Prerequisites (outside this spec)

- `@safedep/` org on npmjs.com with publish rights configured.
- Apple Developer account + signing certificate (for notarization — deferred).

---

## Deferred

- npm publish automation and release workflow integration.
- macOS notarization (Gatekeeper edge cases are low-risk for npm distribution; esbuild ships unsigned with the same posture).
- Additional platforms (`linux/arm`, `win32/arm64`, etc.).
- `--require-tag` enforcement in CI.
