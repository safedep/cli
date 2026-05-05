# safedep auth status

Report which credentials the active profile holds.

## Synopsis

```
safedep auth status [--output rich|plain|agent|json] [--profile <name>]
```

## Output

Reports profile name, tenant, presence of an API key, presence of an OAuth token, and the OAuth access-token expiry decoded from the JWT.

| Mode | Format |
|---|---|
| `rich` | Tabular block with badges for each credential. |
| `plain` | `key: value` lines. |
| `agent` | Single-line `key=value` pairs. |
| `json` | `{profile, tenant, api_key_present, oauth_token_present, oauth_expires_at}`. |

## Exit codes

- `0` always (status is informational, not a probe).
