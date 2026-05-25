# NPM Release Runbook

Operational notes for the npm distribution pipeline. Updated as issues are discovered.

---

## How to release

```bash
git tag v0.1.0
git push origin v0.1.0
```

That is the entire human action required. The `Release Automation` workflow does the rest.

---

## NPM token strategy

The long-term goal is token-less publishing via npm OIDC trusted publishing. The
bootstrap requires one round with a token.

### Round 1 — bootstrap (token required)

Before the first tag push, add these to the `binary-release` environment:

| Secret | Purpose |
|---|---|
| `GORELEASER_GITHUB_TOKEN` | Creates GitHub Release, pushes Homebrew tap |
| `NPM_TOKEN` | Publishes to npm registry (`@safedep` scope, automation token) |
| `SAFEDEP_CLOUD_API_KEY` | PMG supply-chain protection |
| `SAFEDEP_CLOUD_TENANT_DOMAIN` | PMG tenant routing |

After the first release succeeds and all 6 packages exist on npm:
1. Go to npmjs.com → configure trusted publisher for the `@safedep` org,
   linking to the `safedep/cli` repo + `binary-release` environment
2. Delete `NPM_TOKEN` from `binary-release` — it is never needed again

`GORELEASER_GITHUB_TOKEN` must have write access to both `safedep/cli` (create
release, upload tarballs) and `safedep/homebrew-tap` (update `Casks/cli.rb`).
If this is a fine-grained PAT scoped only to `safedep/cli`, goreleaser creates
the GitHub Release then fails during the homebrew step — partial state requiring
manual cleanup. Verify token repository scope before first release.

### Round 2+ — OIDC only (no stored npm token)

npm trusted publishing with OIDC went GA on 2025-07-31. Reference:
https://github.blog/changelog/2025-07-31-npm-trusted-publishing-with-oidc-is-generally-available/

Requires: npm CLI v11.5.1+ (Node 24 ships with npm 11.x — already in workflow).

**Setup (once, after first release):**

The `@safedep` org existing is not sufficient — each package must exist on
npm before trusted publishing can be configured for it. The bootstrap publish
(round 1) creates all 6 packages.

After bootstrap, check whether npmjs.com exposes an org-level trusted
publisher setting under the `@safedep` org settings. If it does, one
configuration covers all current and future `@safedep/*` packages. If it
does not, configure per-package (6 times) on npmjs.com → package settings →
Trusted Publisher:
- Organization: `safedep`
- Repository: `cli`
- Workflow filename: `goreleaser.yml`
- Environment: `binary-release`

**Workflow changes after setup:**

1. Remove `NODE_AUTH_TOKEN` from the `publish-npm` step env.
2. Drop `--provenance` from the `publish-npm` Nx target command —
   trusted publishing adds provenance attestations automatically.

`pnpm publish -r` works for OIDC: pnpm uses `npm-registry-fetch` (npm's
registry library) internally, which performs the OIDC token exchange. The
`id-token: write` permission is already on the job.

**Warning — classic token deprecation:**

npm is revoking classic tokens. The bootstrap `NPM_TOKEN` must be a
**granular access token** (not a classic token). Complete the OIDC migration
promptly after the first release.

---

## What the workflow does

```
git tag push
  └─ release job (environment: binary-release)
       ├─ goreleaser release --clean          (via Nx build-release)
       │    builds: linux/x64, linux/arm64, darwin universal, windows/x64
       │    creates: GitHub Release + tarballs + checksums.txt
       │    updates: safedep/homebrew-tap Casks/cli.rb
       ├─ actions/attest-build-provenance     (signs dist/checksums.txt)
       └─ pnpm nx run safedep-cli:publish-npm (via Nx)
            ├─ sync-binaries --set-version $VERSION
            │    copies binaries into packages/cli-*/bin/
            │    bumps version in all non-private package.json files
            │    verifies each platform package has a non-empty bin/
            ├─ tsdown (builds packages/cli/dist/bin.cjs shim)
            └─ pnpm publish -r --provenance
                 publishes: @safedep/cli-linux-x64, -arm64, -darwin-x64,
                            -darwin-arm64, -win32-x64, @safedep/cli
                 workspace:* in optionalDependencies → rewritten to $VERSION
  └─ test-installation job (needs: release)
       matrix: ubuntu/macos/windows × node 18/20/22/24
       waits for package on registry, installs, runs `safedep version`
```

---

## Known operational issues

### Retry after partial failure

If the `release` job fails AFTER goreleaser succeeds (e.g. npm registry timeout), retrying the job will fail at the goreleaser step — goreleaser v2 refuses to create a GitHub Release that already exists.

**Recovery procedure:**
1. Delete the GitHub Release for the failed tag (UI or `gh release delete vX.Y.Z`)
2. Re-run the workflow job (or re-push the tag)

`pnpm publish -r` is safe to retry — it checks the registry and skips already-published versions, so partially-published platform packages are not re-published.

### Version 0.0.0 in dry-run

The `release-preflight` CI job (runs on every PR) validates the pipeline end-to-end via `pnpm publish -r --dry-run`. It publishes at version `0.0.0` because no real goreleaser release is created in PRs. This means the dry-run does not validate version-setting code paths. Version-setting is covered by the Go unit tests in `scripts/sync-binaries/`.

---

## Pre-release versions (rc, beta)

Not yet supported. The workflow trigger `v[0-9]+.[0-9]+.[0-9]+` does not match `v0.1.0-rc1`. The semver validation in `sync-binaries` also rejects pre-release suffixes (`^\d+\.\d+\.\d+$`).

To add support, three changes are needed:
1. Add tag pattern `"v[0-9]+.[0-9]+.[0-9]+-*"` to the workflow trigger
2. Update `semverRe` in `scripts/sync-binaries/version.go` to accept pre-release suffixes
3. Use `--tag next` (not `--tag latest`) for pre-release npm publishes — otherwise `npm install @safedep/cli` would resolve a pre-release as the default version

---

## Analysis log

### 2026-05-21

Full mental walkthrough of first-time and day-2 release flows. Findings:

- `binary-release` environment missing `NPM_TOKEN`, `SAFEDEP_CLOUD_API_KEY`, `SAFEDEP_CLOUD_TENANT_DOMAIN` — **blocking for first release**
- Retry-after-partial-failure requires manual GitHub Release deletion before goreleaser can re-run
- `fail-fast: false` missing on `test-installation` matrix — fixed
- `GITHUB_TOKEN` passed to `publish-npm` Nx step is redundant — removed
- `workspace:*` rewriting by pnpm at publish time is correct — lock file not re-read during publish
- VERSION env var propagation through Nx chain confirmed correct — nx:run-commands inherits parent env
- `GORELEASER_GITHUB_TOKEN` must have write to both `safedep/cli` and `safedep/homebrew-tap` — fine-grained PATs scoped to one repo will fail mid-release
- PMG `cloud sync` with `if: always()` but no `continue-on-error: true` would mark a successful release as failed — fixed
- `pnpm publish -r` skips already-published versions on retry — safe recovery for partial npm publish

Failure-mode matrix:

| Fails at | GH Release exists | npm published | Recovery |
|---|---|---|---|
| Any setup step | No | No | Fix, re-run |
| goreleaser compile/archive/checksum | No | No | Fix, re-run |
| goreleaser release creation+ | Yes (partial) | No | Delete GH release, fix, re-run |
| Attestation | Yes | No | Delete GH release, re-run |
| tsdown build | Yes | No | Delete GH release, re-run |
| npm partial publish | Yes | Some | Delete GH release, re-run (pnpm skips already-published) |
| PMG cloud sync | Yes | Yes | No action — `continue-on-error: true` |
| test-installation smoke | Yes | Yes | Investigate per OS/Node combo |
