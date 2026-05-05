# safedep auth login

Authenticate with SafeDep Cloud and store credentials in the keychain under the active profile.

By default, runs the OAuth2 device-code flow in the browser, picks an accessible tenant, creates a fresh API key, and stores both the OAuth tokens and the API key in the keychain. Pass `--api-key` to skip the OAuth flow and store an API key you already have.

## Synopsis

```
safedep auth login [flags]
safedep auth login --api-key [--api-key-value <key> | --from-stdin] [--tenant <domain>]
```

## Flags

| Flag | Description |
|---|---|
| `--api-key` | Use static API-key login instead of OAuth2 device flow. |
| `--api-key-value <key>` | Supply the API key inline. Only with `--api-key`; prefer `--from-stdin` or `SAFEDEP_API_KEY`. |
| `--from-stdin` | Read API key from stdin. Only with `--api-key`. |
| `--tenant <domain>` | Tenant domain. Used as fallback when none is stored for the profile. |
| `--api-key-expiry-days <n>` | Expiry for API keys created during device login (default 90). |
| `--rotate-api-key` | Force creation of a new API key during device login. By default, an existing API key for the chosen tenant is preserved. |
| `--no-api-key` | Skip API key creation during device login. OAuth tokens are still stored. |

Inherits root flags `--output` and `--profile`.

## Inputs (precedence)

For `--api-key`:
1. `--api-key-value`
2. `--from-stdin`
3. `SAFEDEP_API_KEY` env var
4. interactive prompt

## Outputs

- Device flow: opens the verification URL in your browser (rich mode) and prints the user code. After the IdP confirms the grant, prints a success line with tenant, profile, and API-key expiry.
- API-key flow: verifies the key against the data plane before storing. Prints success on completion.

## Exit codes

- `0` on success.
- `1` on any failure (cancelled flow, network error, verification failure, missing tenant).
