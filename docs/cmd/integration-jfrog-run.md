# safedep integration jfrog run

Poll SafeDep for verified malicious packages and push them to JFrog XRay as Custom Issues.

## Synopsis

```
safedep integration jfrog run --config <file>
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--config`, `-c` | yes | Path to YAML config file |
| `--profile` | no | Credential profile (inherited from root; defaults to `"default"`) |

## Config file

```yaml
source:
  poll_interval: 60s
  cursor_file: ~/.safedep/integration-jfrog-cursor.json

jfrog:
  url: https://company.jfrog.io
  access_token: TOKEN  # or set SAFEDEP_JFROG_ACCESS_TOKEN env var
```

## Authentication

Uses the SafeDep API key for the active profile. Run `safedep auth login` first, or set
`SAFEDEP_API_KEY` and `SAFEDEP_TENANT_ID` environment variables for headless environments.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Daemon stopped cleanly (SIGINT / SIGTERM) |
| 1 | Fatal error (config invalid, auth failed, unrecoverable poll error) |
