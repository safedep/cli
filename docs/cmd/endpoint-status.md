# safedep endpoint status

Show tenant-wide endpoint fleet health in a trailing time window.

## Synopsis

```
safedep endpoint status [--since DURATION] [--output table|plain|json] [--profile <name>] [--insecure-keychain-fallback]
```

## Description

`endpoint status` queries SafeDep Cloud for an aggregate health snapshot of all endpoints that have reported to the tenant. The snapshot covers the trailing time window controlled by `--since`.

The response includes total registered endpoints, those that have synced at least once in the window (active), those that have not (silent), total event volume across all tools, and the count of package-guard blocked installs.

## Flags

| Flag | Default | Description |
|---|---|---|
| `--since` | `24h` | Trailing window length parsed as a Go duration string (e.g. `24h`, `168h`, `30m`). A non-positive value lets the server apply its own default window. |
| `--output` | `table` | Output format: `table`, `plain`, or `json`. Inherited from the root command. |
| `--profile` | `default` | Credential profile to use. Inherited from the root command. |
| `--insecure-keychain-fallback` | `false` | Allow falling back to an unencrypted credential store. Inherited from the root command. |

## Examples

Show fleet health for the past 24 hours (default):

```
safedep endpoint status
```

Show fleet health for the past seven days:

```
safedep endpoint status --since 168h
```

Emit JSON for downstream processing:

```
safedep endpoint status --output json
```

## Output

| Mode | Format |
|---|---|
| `table` | Two-column table with metric names and their values. |
| `plain` | Tab-separated `metric\tvalue` lines, one per metric. |
| `json` | Object with fields `TotalEndpoints`, `ActiveEndpoints`, `SilentEndpoints`, `TotalEvents`, `PMGBlockedEvents`. |

## Exit codes

- `0` on success.
- Non-zero on authentication failure, network error, or a server-side error.
