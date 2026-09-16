# safedep auth token

Print the OAuth access token for the active SafeDep profile. The command writes
the token to stdout. External tools and scripts use the token to call the SafeDep
control plane.

## Synopsis

```
safedep auth token
```

## Description

The command writes the OAuth access token to stdout. It writes nothing else.

If the token has expired, the command refreshes it first. So the token is always
valid when it prints. The command never prints the refresh token.

Use the token to authenticate a script or a tool. The script does not need to do
the SafeDep login or the token refresh:

```bash
export SAFEDEP_TOKEN=$(safedep auth token)
```

The token is per profile. Use `--profile` to print the token for a different
profile.

## Requirements

- Log in first with OAuth. Run [`safedep auth login`](./auth-login.md).
- An API-key login does not create an OAuth token. This command needs the OAuth
  login.

## Notes

- The access token has a short life. Do not cache it. Run `safedep auth token`
  again to get a new token. A long-running program must get a new token before
  the current token expires.
- The token is a bearer credential. Do not pass it as a command argument. A
  command argument goes into the shell history and the process list. Put the
  token in an environment variable, as the example shows.
