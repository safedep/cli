# safedep integration jfrog cursor reset

Clear the saved feed cursor for the active SafeDep profile so the next run starts fresh.

## Synopsis

```
safedep integration jfrog cursor reset
```

## Description

`run` stores a cursor to resume where it stopped. Reset it to re-process the
feed from scratch: the next run starts from now, or from `--backfill`.

Reset after a dry-run. `run --dry-run` advances the same cursor, so reset it
before the first real run or that run skips what the preview consumed.

The cursor is per profile. Use `--profile` to target a different one.

## Examples

```bash
# Active profile
safedep integration jfrog cursor reset

# A named profile
safedep --profile customer-a integration jfrog cursor reset
```

## Notes

The command prints the SQLite path it uses. Editing that file by hand is not
supported; use `cursor reset` instead.
