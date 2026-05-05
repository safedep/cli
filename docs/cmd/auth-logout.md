# safedep auth logout

Remove the credentials stored for the active profile from the keychain.

> Status: not yet implemented. The command is wired into the CLI tree but returns an error on use.

## Synopsis

```
safedep auth logout [--profile <name>]
```

## Flags

Inherits root flags `--output` and `--profile`.

## Exit codes

- `0` on success.
- `1` on any failure.
