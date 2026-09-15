# safedep integration crowdstrike cursor remove

Remove the saved sync cursor for the active SafeDep profile so the next run starts fresh.

## Synopsis

```
safedep integration crowdstrike cursor remove
```

## Description

`run` stores a cursor to resume where it stopped. Remove it to start fresh: the
next run starts from now, or from `--backfill`.

The cursor is per profile. Use `--profile` to target a different one.

## Examples

```bash
# Active profile
safedep integration crowdstrike cursor remove

# A named profile
safedep --profile customer-a integration crowdstrike cursor remove
```

## Notes

The command prints the SQLite path it uses. Editing that file by hand is not
supported. Use `cursor remove` or `cursor set` instead.
