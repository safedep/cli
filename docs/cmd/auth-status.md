# safedep auth status

Report whether the active profile holds valid credentials and which tenant they bind to.

> Status: not yet implemented. The command is wired into the CLI tree but returns an error on use.

## Synopsis

```
safedep auth status [--profile <name>]
```

## Flags

Inherits root flags `--output` and `--profile`.

## Exit codes

- `0` on success.
- `1` on any failure.
