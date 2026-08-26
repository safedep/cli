# safedep integration jfrog run

Long-running daemon that streams verified malicious packages from the SafeDep
ThreatIntel Feed and pushes them to JFrog XRay as Custom Issues. When XRay has a
blocking policy configured, packages flagged by SafeDep are automatically
blocked for all developers using that JFrog instance.

## Synopsis

```
safedep integration jfrog run --instance-url <url> --instance-access-token <token>
```

## Quick start

```bash
# 1. Authenticate with SafeDep (once)
safedep auth login

# 2. Run with flags
safedep integration jfrog run \
  --instance-url https://yourcompany.jfrog.io \
  --instance-access-token YOUR_JFROG_TOKEN

# Or use environment variables (recommended for CI / server deployments)
export SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_URL=https://yourcompany.jfrog.io
export SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_ACCESS_TOKEN=YOUR_JFROG_TOKEN
safedep integration jfrog run
```

## Flags

| Flag | Required | Default | Description |
|---|---|---|---|
| `--instance-url` | yes* | — | JFrog instance base URL. Must be `https://`. |
| `--instance-access-token` | yes* | — | JFrog access token scoped to XRay. |
| `--poll-interval` | no | `5m` | Sleep duration between feed drains (`30s`, `5m`, `1h`). |
| `--backfill` | no | `0` | First-run window used to seed the cursor. `0` starts fresh from now. |
| `--dry-run` | no | `false` | Preview the feed and print what would be pushed, without sending to JFrog. See [Dry run](#dry-run). |
| `--profile` | no | `"default"` | SafeDep credential profile (inherited from root). |

*Required unless the corresponding environment variable is set.

`--backfill` takes a Go duration, so use hours for multi-day windows (e.g.
`--backfill 168h` for 7 days). It only affects the **first** run on a fresh
install: with no stored cursor, the command requests reports updated after
`now - backfill`. Once a cursor exists, `--backfill` is ignored and the command
resumes from the last processed report. At startup the command logs which mode
it uses: resuming from the saved cursor, or starting fresh (with the backfill
window when set).

## Environment variables

Flags take precedence. Environment variables are the fallback, useful for server
deployments or CI where passing secrets as CLI arguments is undesirable.

| Variable | Corresponding flag |
|---|---|
| `SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_URL` | `--instance-url` |
| `SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_ACCESS_TOKEN` | `--instance-access-token` |

`--backfill` is a flag only: it is a one-time first-run window, not a secret, so
it has no environment variable.

## Dry run

`--dry-run` previews the feed and prints each finding as a `Would push:` line,
without sending to JFrog. Use it to check the feed before connecting a JFrog
instance.

It tests the whole pipeline as-is. The only difference from a real run is the
destination: it prints instead of calling JFrog.

- No JFrog credentials needed.
- `--backfill` works the same as a real run (default `0`). Pass `--backfill 24h`
  to preview recent history.
- It advances the same saved cursor. Run
  [`cursor remove`](./integration-jfrog-cursor-remove.md) before the first real
  run, or that run skips what the preview consumed.

```bash
# Preview the last 24 hours, then run for real
safedep integration jfrog run --dry-run --backfill 24h
safedep integration jfrog cursor remove
safedep integration jfrog run --instance-url https://yourcompany.jfrog.io --instance-access-token YOUR_JFROG_TOKEN
```

## Behaviour

- **First run.** With no stored cursor the command starts fresh from now
  (`--backfill 0`). It does not pull historical reports unless you set
  `--backfill`.
- **Malicious only.** Suspicious packages are dropped by the feed before they
  reach the command. Only malicious reports are pushed to XRay, the same as the
  previous CLI.
- **Withdrawn reports.** When SafeDep retracts a report (for example a false
  positive), the feed re-delivers it as withdrawn. The command deletes the
  matching XRay Custom Issue by its reproducible id. A delete for an issue that
  is already gone is treated as success.
- **Already present.** On restart or an overlapping `--backfill`, the command
  re-pushes reports it pushed before. XRay reports the issue already exists; the
  command treats that as success (already present), the same way it treats an
  already-gone delete.
- **Resume.** The cursor is stored per SafeDep profile. Restarting the command
  resumes from the last processed report. Switching `--profile` switches the
  cursor. Re-process from a chosen point with
  [`cursor set`](./integration-jfrog-cursor-set.md), or from scratch with
  [`cursor remove`](./integration-jfrog-cursor-remove.md).

## Issue id

Each Custom Issue id is `SD-` followed by the SafeDep report id, for example
`SD-01KR0EKN6PMW0ZRFRN992H1PKX`. The id is stable and reproducible, so an admin
can map an XRay issue back to a SafeDep report. Reports whose id would exceed
JFrog's 32-character limit are skipped with a warning.

## JFrog access token

Requires **Manage Xray Metadata** permission on your JFrog instance.

## SafeDep authentication

The feed reads the SafeDep data plane (`api.safedep.io`), which authenticates
with an **API key** that belongs to your tenant. Authenticate with one of:

```bash
# API key login
safedep auth login --tenant your-tenant.safedep.io --api-key --api-key-value YOUR_API_KEY

# or environment variables (no login needed)
export SAFEDEP_TENANT_ID=your-tenant.safedep.io
export SAFEDEP_API_KEY=YOUR_API_KEY
```

Credentials are resolved in this order:

1. `SAFEDEP_API_KEY` + `SAFEDEP_TENANT_ID` environment variables
2. Keychain credentials stored by `safedep auth login`

An OAuth-only login does not provide the data-plane API key, so the command
stops with a message telling you to authenticate with an API key.

Use `--profile` to switch between multiple SafeDep tenants:

```bash
safedep --profile customer-a integration jfrog run \
  --instance-url https://customer.jfrog.io \
  --instance-access-token $TOKEN
```

## JFrog XRay setup

Ensure your JFrog XRay instance has a **Malware** security policy with a block
action. SafeDep pushes findings as Custom Issues with `issue_kind: 1` (malicious
package). Without a policy, issues are recorded but packages are not blocked.

## Subscription

This integration is available with the **Threat Intel Feed add-on**. If the
tenant does not have it, the command stops with:

```
The SafeDep JFrog XRay integration is available with the Threat Intel Feed add-on, which is not enabled for this tenant. See the pricing page: https://safedep.io/pricing/#threat-intel
```

See the [pricing page](https://safedep.io/pricing/#threat-intel) to enable the add-on.
