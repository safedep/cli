# safedep integration crowdstrike cursor set

Set the saved sync cursor to a timestamp for the active SafeDep profile.

## Synopsis

```
safedep integration crowdstrike cursor set <timestamp>
```

## Description

The next run processes events after `<timestamp>`. Use it to re-process from a
chosen point, a more precise alternative to `--backfill`.

`<timestamp>` is RFC3339, for example `2026-08-25T10:00:00Z`. The cursor is per
profile. Use `--profile` to target a different one.

## Examples

```bash
# Re-process everything since 1 August
safedep integration crowdstrike cursor set 2026-08-01T00:00:00Z

# A named profile
safedep --profile customer-a integration crowdstrike cursor set 2026-08-01T00:00:00Z
```

## See also

- [`cursor remove`](./integration-crowdstrike-cursor-remove.md) clears the cursor.
