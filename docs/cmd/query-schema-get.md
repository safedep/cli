# safedep query schema get

Fetch the SQL schema served by SafeDep Cloud, listing each table and its columns.

## Synopsis

```
safedep query schema get [--output table|plain|json] [--profile <name>]
```

## Output

For each queryable table, prints one entry per column with the column's selectability, filterability, requiredness, and reference URL.

| Mode | Format |
|---|---|
| `table` | Rendered table: Table, Column, Selectable, Filterable, Reference. |
| `plain` | One line per column: `<table>.<col>\t<flags>\t<reference_url>`. |
| `json` | `{ "tables": [{ "name": ..., "columns": [{ "name": ..., "selectable": ... }] }] }`. |

Tables and columns are sorted alphabetically so output is stable across runs.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

- `0` on success.
- `1` on any failure: missing credentials, server-side error.
