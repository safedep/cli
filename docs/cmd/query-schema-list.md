# safedep query schema list

List the queryable tables in the SafeDep Cloud query schema with column counts and descriptions. Use this for quick orientation; reach for `safedep query schema show <table>` to inspect one table in depth, or `safedep query schema get -o json` for the full machine-readable schema.

## Synopsis

```
safedep query schema list [--output table|plain|json] [--profile <name>]
```

## Output

| Mode | Format |
|---|---|
| `table` | A `Table / Columns / Description` table preceded by the total table count and followed by a tip pointing to `schema show`. |
| `plain` | One line per table: `<name>\t<column_count>\t<description>`. |
| `json` | `{ "tables": [{ "name": "...", "columns": N, "description": "..." }, ...] }`. |

Tables are sorted alphabetically so output is stable across runs.

## Examples

```bash
safedep query schema list                 # quick orientation
safedep query schema list -o plain | awk -F'\t' '$2 >= 5'  # tables with five or more columns
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

- `0` on success.
- `1` on any failure: missing credentials, server-side error.
