# safedep integration jfrog run

Long-running daemon that polls SafeDep for verified malicious packages and pushes them
to JFrog XRay as Custom Issues. When XRay has a blocking policy configured, packages
flagged by SafeDep are automatically blocked for all developers using that JFrog instance.

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
| `--poll-interval` | no | `60s` | Sleep duration between poll cycles (`30s`, `5m`, `1h`). |
| `--cursor-file` | no | `~/.safedep/integration-jfrog-cursor.json` | Path to the cursor file. |
| `--profile` | no | `"default"` | SafeDep credential profile (inherited from root). |

*Required unless the corresponding environment variable is set.

## Environment variables

Flags take precedence. Environment variables are the fallback — useful for server
deployments or CI where passing secrets as CLI arguments is undesirable.

| Variable | Corresponding flag |
|---|---|
| `SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_URL` | `--instance-url` |
| `SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_ACCESS_TOKEN` | `--instance-access-token` |

## JFrog access token

Requires **Manage Xray Metadata** permission on your JFrog instance.

## SafeDep authentication

Credentials are resolved in this order:

1. `SAFEDEP_API_KEY` + `SAFEDEP_TENANT_ID` environment variables
2. Keychain credentials stored by `safedep auth login`

Use `--profile` to switch between multiple SafeDep tenants:

```bash
safedep --profile customer-a integration jfrog run \
  --instance-url https://customer.jfrog.io \
  --instance-access-token $TOKEN
```

## Cursor file

Tracks the timestamp of the last processed record so polling resumes from the correct
point after a restart. Created automatically on first run.

To go back in history, edit the file directly:

```bash
# Reprocess from a specific date
echo '{"last_seen_at":"2026-04-01T00:00:00Z"}' > ~/.safedep/integration-jfrog-cursor.json

# Reprocess everything from the beginning
rm ~/.safedep/integration-jfrog-cursor.json
```

## JFrog XRay setup

Ensure your JFrog XRay instance has a **Malware** security policy with a block action.
SafeDep pushes findings as Custom Issues with `issue_kind: 1` (malicious package).
Without a policy, issues are recorded but packages are not blocked.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Stopped cleanly (SIGINT / SIGTERM) |
| 1 | Fatal error: missing required config, auth failed, or unrecoverable error |
