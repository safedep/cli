# safedep query schema show

Show a single table from the SafeDep Cloud query schema: its columns with types, capability flags, enum values, and the joins that involve it in either direction.

## Synopsis

```
safedep query schema show <table> [--output table|plain|json] [--profile <name>]
```

## Output

| Mode | Format |
|---|---|
| `table` | Table header (name, description, optional time-window metadata), a compact `Column / Type / Caps / Notes` table with truncated enum previews and a per-table `refs:` footnote when reference URLs are present, followed by a `Joins` list of edges involving the table. |
| `plain` | One line per column: `<table>.<col>\t<type>\t<caps>\t<enum-csv>`, then `# join: <from> -> <to> (<cardinality>)` for each edge. |
| `json` | `{ "table": { ...full table object... }, "edges": [...] }`. The `table` object matches the per-table shape returned by `schema get`; `edges` is narrowed to those where this table appears at either endpoint. |

Unknown table names produce an error that lists the available tables.

### Joins

`schema show` widens the edge filter compared to `schema get --table`: it returns edges where the named table appears at *either* endpoint, since the goal of a single-table view is discovery. (`schema get --table X --table Y` keeps the stricter "both endpoints in the filter" rule because there it is filtering a multi-table view.)

### Caps codes

`sel` = selectable, `fil` = filterable, `grp` = groupable, `agg` = aggregatable, `idx` = indexed. The `plain` mode uses the long-form names for backward-compatible scripting.

## Examples

```bash
safedep query schema show endpoints
safedep query schema show packages -o json
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

- `0` on success.
- `1` on any failure: missing credentials, unknown table name, server-side error.
