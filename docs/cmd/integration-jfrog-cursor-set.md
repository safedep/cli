# safedep integration jfrog cursor set

Set the saved feed cursor to a timestamp for the active SafeDep profile.

## Synopsis

```
safedep integration jfrog cursor set <timestamp>
```

## Description

The next run processes reports updated strictly after `<timestamp>`. Use it to
re-process the feed from a chosen point, a more precise alternative to
`--backfill`.

`<timestamp>` is RFC3339, for example `2026-08-25T10:00:00Z`. The cursor is per
profile. Use `--profile` to target a different one.

## Examples

```bash
# Re-process everything updated since 1 August
safedep integration jfrog cursor set 2026-08-01T00:00:00Z

# A named profile
safedep --profile customer-a integration jfrog cursor set 2026-08-01T00:00:00Z
```

## See also

- [`cursor remove`](./integration-jfrog-cursor-remove.md) clears the cursor.
