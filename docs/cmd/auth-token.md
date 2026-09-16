# safedep auth token

Print the OAuth access token for the active SafeDep profile to stdout, for use by
external tools and scripts that call the SafeDep control plane directly.

## Synopsis

```
safedep auth token
```

## Description

Prints the active profile's OAuth **access token** to stdout, nothing else. The
token is refreshed first if it has expired, so what prints is always usable. The
**refresh token is never printed**.

Use it to authenticate a script or tool without reimplementing SafeDep login and
token refresh:

```bash
export SAFEDEP_TOKEN=$(safedep auth token)
```

The cursor is per profile; use `--profile` to print a different profile's token.

## Requirements

- A prior OAuth login: run [`safedep auth login`](./auth-login.md) first.
- An **API-key** login does not produce an OAuth token, so this command needs the
  OAuth (device) login.

## Notes

- Access tokens are short-lived. Re-run `safedep auth token` to get a fresh one
  rather than caching it; a long-running consumer should fetch a new token when
  its current one nears expiry.
- The token is a bearer credential. Avoid passing it as a literal argument (it
  lands in shell history and the process list); capture it into an environment
  variable as shown above.
