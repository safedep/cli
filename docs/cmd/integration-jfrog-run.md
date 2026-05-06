# safedep integration jfrog run

Long-running daemon that polls SafeDep for verified malicious packages and pushes them
to JFrog XRay as Custom Issues. When XRay has a blocking policy configured, packages
flagged by SafeDep are automatically blocked for all developers using that JFrog instance.

## Synopsis

```
safedep integration jfrog run --config <file>
```

## Quick start

```bash
# 1. Authenticate with SafeDep (once)
safedep auth login

# 2. Create a config file
cat > config.yml <<EOF
jfrog:
  url: https://yourcompany.jfrog.io
  access_token: YOUR_JFROG_TOKEN
EOF

# 3. Run
safedep integration jfrog run --config config.yml
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--config`, `-c` | yes | Path to YAML config file |
| `--profile` | no | SafeDep credential profile (defaults to `"default"`) |

## Config file reference

```yaml
source:
  poll_interval: 60s
  cursor_file: ~/.safedep/integration-jfrog-cursor.json

jfrog:
  url: https://yourcompany.jfrog.io
  access_token: YOUR_JFROG_TOKEN
```

### `source` section

| Field | Default | Description |
|---|---|---|
| `poll_interval` | `60s` | How long to sleep between poll cycles. Accepts Go duration strings: `30s`, `5m`, `1h`. |
| `cursor_file` | `~/.safedep/integration-jfrog-cursor.json` | Path to the cursor file that tracks the last processed record. Created automatically on first run. Delete it to reprocess from the beginning. |

### `jfrog` section

| Field | Required | Description |
|---|---|---|
| `url` | yes | JFrog instance base URL. Must be `https://`. No trailing slash. |
| `access_token` | yes* | JFrog access token scoped to XRay. See below for env var alternative. |

## JFrog access token

The JFrog access token requires **Manage Xray Metadata** permission on your JFrog instance.

You can provide it in two ways:

**Option 1 — config file** (simple, but keep the file out of source control):
```yaml
jfrog:
  access_token: YOUR_TOKEN
```

**Option 2 — environment variable** (recommended for CI or shared machines):
```bash
export SAFEDEP_JFROG_ACCESS_TOKEN=YOUR_TOKEN
safedep integration jfrog run --config config.yml
```

The environment variable takes precedence over the config file value. If both are set,
the env var wins. If neither is set, the command exits with an error at startup.

## SafeDep authentication

The command uses your SafeDep API key to poll for malicious packages. Credentials are
resolved in this order:

1. `SAFEDEP_API_KEY` + `SAFEDEP_TENANT_ID` environment variables
2. Keychain credentials stored by `safedep auth login`

For interactive use, run `safedep auth login` once. For CI or headless environments,
set the environment variables.

Use `--profile` to switch between multiple SafeDep tenants:

```bash
safedep --profile customer-a integration jfrog run --config config.yml
```

## Cursor and restarts

The cursor file stores the timestamp of the last processed record. On restart, polling
resumes from that point — no records are skipped or duplicated.

To reprocess all records from the beginning, delete the cursor file:

```bash
rm ~/.safedep/integration-jfrog-cursor.json
```

## JFrog XRay setup

Before running this command, ensure your JFrog XRay instance has a **Malware** security
policy with a block action. SafeDep pushes findings as Custom Issues with `issue_kind: 1`
(malicious package). Without a policy, the issues are recorded but packages are not blocked.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Stopped cleanly (SIGINT / SIGTERM) |
| 1 | Fatal error: config invalid, auth failed, or unrecoverable error |
