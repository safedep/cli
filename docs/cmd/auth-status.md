# safedep auth status

Report which credentials the active profile holds.

## Synopsis

```
safedep auth status [--output table|plain|json] [--profile <name>]
```

## Output

Reports profile name, tenant, the API key and its recorded expiry, presence of an OAuth token, and the OAuth access-token expiry decoded from the JWT. Status reads the keychain only. It does not call SafeDep Cloud.

The API key has one of these states:

| State | Table badge | Meaning |
|---|---|---|
| `valid` | `configured` | The recorded expiry is more than 14 days away. |
| `expiring` | `expires soon` | The key expires within 14 days. `safedep auth login` replaces it. |
| `expired` | `expired` | The recorded expiry has passed. pmg and vet fail with this key. |
| `expiry_unknown` | `expiry unknown` | No expiry is recorded, for example for a key saved by `--api-key` or by an earlier CLI. Status cannot tell whether it has expired. `safedep auth login` replaces it with a key whose expiry is recorded. |
| `missing` | `not configured` | The profile holds no API key. |

The overall status is `authenticated` only when the OAuth token is valid and the API key has not expired.

| Mode | Format |
|---|---|
| `table` | Tabular block with badges for each credential. |
| `plain` | `key: value` lines, with `API key state` and `Key expires` lines for an API key. |
| `json` | `{profile, tenant, api_key_present, api_key_state, api_key_expires_at, oauth_token_present, oauth_expires_at}`. |

## Exit codes

- `0` always (status is informational, not a probe).
