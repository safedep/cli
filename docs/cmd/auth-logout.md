# safedep auth logout

Remove every credential field stored for the active profile (API key, OAuth access token, refresh token, tenant).

## Synopsis

```
safedep auth logout [--profile <name>]
```

## Flags

Inherits root flags `--output` and `--profile`.

## Behaviour

Calls `Clear` on the keychain for the active profile. Other profiles are unaffected.

## Exit codes

- `0` on success.
- `1` if the keychain cannot be opened or cleared.
