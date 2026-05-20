# sync-binaries

Copies goreleaser-produced binaries into the npm platform packages and
optionally stamps the npm package version. It bridges goreleaser's
`dist/artifacts.json` with the npm distribution pipeline.

## Usage

```
go run ./scripts/sync-binaries/ [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--artifacts-path` | `dist/artifacts.json` | Path to goreleaser artifacts.json |
| `--packages-path` | `./packages` | Path to the npm packages directory |
| `--strict` | `true` | Fail if a package directory does not exist for a built artifact |
| `--set-version` | `` | Semver x.y.z to write into all non-private package.json files |
| `--verify-bins` | `false` | Verify each platform package has a non-empty bin/ after sync |

## Nx targets

| Target | When |
|---|---|
| `sync-binaries:run` | Local dev — reads from a `goreleaser build --snapshot` output |
| `sync-binaries:run-release` | CI release — reads from `goreleaser release` output; also sets version |

## Platform package layout

Goreleaser artifacts map to npm package directories under `packages/`:

| goreleaser goos/goarch | npm package |
|---|---|
| `linux/amd64` | `packages/cli-linux-x64` |
| `linux/arm64` | `packages/cli-linux-arm64` |
| `darwin/all` (universal) | `packages/cli-darwin-x64` and `packages/cli-darwin-arm64` |
| `windows/amd64` | `packages/cli-win32-x64` |

The `--strict` flag (default on) ensures that if goreleaser adds a new
platform target, the build fails fast until the corresponding
`packages/cli-{os}-{arch}/` directory is created.
