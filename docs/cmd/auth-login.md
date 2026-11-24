# safedep auth login

Authenticate with SafeDep Cloud and store credentials in the keychain under the active profile.

By default, runs the OAuth2 device-code flow in the browser, picks an accessible tenant, makes sure the profile holds a valid API key, and stores the OAuth tokens and the API key in the keychain. Pass `--api-key` to skip the OAuth flow and store an API key you already have.

## API key

The device flow keeps the API key that the keychain holds for the profile only when all of these hold:

1. The key belongs to the tenant that you picked.
2. Its recorded expiry is more than 14 days away.
3. SafeDep Cloud accepts it.

Otherwise login creates a new key and prints why, for example `Created a new API key: the stored API key expired on 2026-11-20.` The key is saved with its id and expiry, so the next login and `safedep auth status` can tell when it expires.

A key with no recorded expiry, such as a key saved with `--api-key` or by an earlier CLI, is replaced, because `safedep auth status` cannot warn before it expires.

Login does not revoke the key that it replaces. That key stops working at its own expiry, and any tool that still holds a copy of it, such as an MCP client set up with `safedep protect mcp install`, keeps working until then. SafeDep Cloud still sends the expiry email for it. Delete it in app.safedep.io to revoke it at once.

Login keeps the stored key and prints a warning when SafeDep Cloud cannot be reached.

When login cannot create a new key, for example for a user who cannot create keys:

- If SafeDep Cloud still accepts the stored key, login keeps it and prints a warning.
- Otherwise login stores the OAuth tokens, prints a warning and exits with success. `safedep auth status` then shows the state of the old key. pmg and vet need a key: ask a tenant admin for one and save it with `safedep auth login --api-key`.
- With `--rotate-api-key`, login fails and stores nothing.

pmg and vet read the API key from the keychain. Login checks the keychain key, not `SAFEDEP_API_KEY`.

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
| `--api-key-expiry-days <n>` | Expiry for API keys created during device login (default 90). Must be more than 14, because login replaces a key that expires within 14 days. With `--rotate-api-key`, any value is accepted. |
| `--rotate-api-key` | Create a new API key during device login even when the stored key is valid. |
| `--no-api-key` | Skip API key creation during device login. OAuth tokens are still stored. |

Inherits root flags `--output`, `--profile`, and `--insecure-keychain-fallback`.

## Inputs (precedence)

For `--api-key`:
1. `--api-key-value`
2. `--from-stdin`
3. `SAFEDEP_API_KEY` env var
4. interactive prompt

## Outputs

- Device flow: opens the verification URL in your browser (rich mode) and prints the user code. After the IdP confirms the grant, prints a success line with tenant, profile, and the expiry of the API key that it created or kept.
- API-key flow: verifies the key against the data plane before storing. Prints success on completion.

## Exit codes

- `0` on success.
- `1` on any failure (cancelled flow, network error, verification failure, missing tenant).
