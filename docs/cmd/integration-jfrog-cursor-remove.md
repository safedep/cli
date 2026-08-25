# safedep integration jfrog cursor remove

Remove the saved feed cursor for the active SafeDep profile so the next run starts fresh.

## Synopsis

```
safedep integration jfrog cursor remove
```

## Description

`run` stores a cursor to resume where it stopped. Remove it to start fresh: the
next run starts from now, or from `--backfill`.

Remove after a dry-run. `run --dry-run` advances the same cursor, so remove it
before the first real run or that run skips what the preview consumed.

The cursor is per profile. Use `--profile` to target a different one.

## Examples

```bash
# Active profile
safedep integration jfrog cursor remove

# A named profile
safedep --profile customer-a integration jfrog cursor remove
```

## Notes

The command prints the SQLite path it uses. Editing that file by hand is not
supported; use `cursor remove` or `cursor set` instead.
